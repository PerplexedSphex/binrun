#!/usr/bin/env python3
"""
build_outreach_files.py  ───────────────────────────────────────────────────────
Fast **drop‑in replacement** for the original outreach file builder.

Creates the same two CSVs *without changing any column or value semantics*:
  • **contact_compliance_summary.csv**   (one row per contact‑email × account)
  • **account_compliance_summary.csv**   (one row per account)

80 % of the speed‑ups, zero behaviour drift:
1.  Window filtering done once; per‑group DataFrames cached.
2.  Boolean helpers & categorical down‑casts before any grouping.
3.  Legacy composite PK columns restored (`eval_pk`, `viol_pk`, `enf_pk`).
4.  Progress prints with emojis so you can watch the run.
"""
from __future__ import annotations

import os
from pathlib import Path
from datetime import date

import duckdb  # type: ignore
import pandas as pd
import numpy as np
from dotenv import load_dotenv

# ──────────────────────────── env & paths ────────────────────────────
SCRIPT_DIR = Path(__file__).parent.resolve()
load_dotenv(SCRIPT_DIR / ".env")

DB_PATH        = Path(os.getenv("DB_PATH",  SCRIPT_DIR.parent.parent / "store" / "db" / "rcrainfo.duckdb"))
OUTPUT_DIR     = Path(os.getenv("OUTPUT_DIR", SCRIPT_DIR.parent.parent / "store" / "script_output_files"))
YEARS_LOOKBACK = int(os.getenv("YEARS_LOOKBACK", "5"))

OUTPUT_DIR.mkdir(parents=True, exist_ok=True)

current_year    = date.today().year
window_year_min = current_year - YEARS_LOOKBACK         # first calendar year in window
window_year_max = current_year - 1                      # most recent *complete* year

print(f"🦆  DB …  {DB_PATH}", flush=True)
print(f"📅  Window {window_year_min}-01-01 → {window_year_max}-12-31", flush=True)

# ─────────────────────────── SQL pull ───────────────────────────────
BASE_SQL = """
SELECT
    handler_id,
    eval_activity_location, eval_identifier, eval_start_date, eval_agency,
    viol_activity_location, viol_seq, viol_determined_by_agency,
    enf_activity_location,  enf_identifier,  enf_action_date,  enf_agency,

    CAST(eval_start_date  AS INT) AS eval_date_i,
    CAST(determined_date  AS INT) AS viol_date_i,
    CAST(enf_action_date  AS INT) AS enf_date_i,

    eval_type_desc, found_violation, viol_short_desc,
    CASE WHEN actual_rtc_date IS NOT NULL AND determined_date IS NOT NULL
         THEN DATE_DIFF('day', STRPTIME(actual_rtc_date,'%Y%m%d'), STRPTIME(determined_date,'%Y%m%d'))
    END AS days_to_resolve,

    enf_type_desc,
    CAST(NULLIF(final_amount,'') AS DOUBLE) AS penalty_amt,

    account_id, account_name, tier_2025,
    parent_account_id, parent_account,
    account_owner, bda_owner, cs_owner,
    COALESCE(contact_email_address,'(no email)') AS contact_email
FROM ce_reporting
JOIN account_matched_facilities USING (handler_id)
WHERE account_id IS NOT NULL
"""

with duckdb.connect(database=DB_PATH, read_only=True) as con:
    con.execute(f"SET threads TO {os.cpu_count()}")
    con.execute("SET enable_progress_bar = true")
    base: pd.DataFrame = con.execute(BASE_SQL).fetchdf()

print(f"📥  Pulled {len(base):,} rows", flush=True)

# ────────────────────── restore legacy composite PKs ───────────────────
base["eval_pk"] = (
    base["handler_id"].astype(str) + "|" +
    base["eval_activity_location"].astype(str) + "|" +
    base["eval_identifier"].astype(str) + "|" +
    base["eval_start_date"].astype(str) + "|" +
    base["eval_agency"].astype(str)
)
base["viol_pk"] = (
    base["handler_id"].astype(str) + "|" +
    base["viol_activity_location"].astype(str) + "|" +
    base["viol_seq"].astype(str) + "|" +
    base["viol_determined_by_agency"].astype(str)
)
base["enf_pk"] = (
    base["handler_id"].astype(str) + "|" +
    base["enf_activity_location"].astype(str) + "|" +
    base["enf_identifier"].astype(str) + "|" +
    base["enf_action_date"].astype(str) + "|" +
    base["enf_agency"].astype(str)
)

# ──────────────────── categoricals & helpers ───────────────────────────
for cat_col in [
    "eval_type_desc", "viol_short_desc", "enf_type_desc",
    "tier_2025", "account_owner", "bda_owner", "cs_owner",
]:
    base[cat_col] = base[cat_col].astype("category")

base["eval_year"] = (base["eval_date_i"] // 10000).astype("Int64")
base["viol_year"] = (base["viol_date_i"] // 10000).astype("Int64")
base["enf_year"]  = (base["enf_date_i"]  // 10000).astype("Int64")

base["is_violation"]      = base["found_violation"] == "Y"
base["is_open_violation"] = base["days_to_resolve"].isna()

window = base[(base["eval_year"] >= window_year_min) & (base["eval_year"] <= window_year_max)].copy()
print(f"🔎  Window rows: {len(window):,}", flush=True)

df_empty = window.head(0)  # keeps the full column set, zero rows

win_contact_groups = {
    k: df for k, df in window.groupby([
        "contact_email", "account_id"], dropna=False, observed=True)
}
win_account_groups = {
    acc: df for acc, df in window.groupby("account_id", observed=True)
}
print("📚  Group caches built", flush=True)

# ────────────────────── helper math funcs ─────────────────────────────

def slope_pct(years: pd.Series, vals: pd.Series) -> float:
    years = years.astype(float)
    if len(years.unique()) < 2 or vals.sum() == 0:
        return 0.0
    base_val = vals.iloc[0] if isinstance(vals, pd.Series) else vals[0]
    if base_val == 0:
        return 0.0
    m = np.polyfit(years, vals, 1)[0]
    return round(100 * m / base_val, 2)


def spike_year(years: pd.Series | np.ndarray, vals: pd.Series | np.ndarray, thresh: float = 2):
    # Accept either pandas Series or NumPy array input for `vals` and `years`.
    if isinstance(vals, np.ndarray):
        vals_arr = vals
    else:  # pandas Series
        vals_arr = vals.to_numpy()

    if vals_arr.std(ddof=0) == 0:
        return None

    z = (vals_arr - vals_arr.mean()) / vals_arr.std(ddof=0)
    idx = int(z.argmax())  # position of max z-score

    if z[idx] < thresh:
        return None

    # Return the corresponding year value
    if isinstance(years, np.ndarray):
        return int(years[idx])
    else:
        return int(years.iloc[idx])


def emerging_type(df: pd.DataFrame, recent_start: int, type_col: str, cnt_col: str):
    recent = df[df["year"] >= recent_start]
    early  = df[df["year"] <  recent_start]
    if recent.empty or early.empty:
        return None
    recent_tot = recent[cnt_col].sum()
    early_tot  = early[cnt_col].sum() or 1
    ratio = (
        (recent.groupby(type_col, observed=True)[cnt_col].sum() / recent_tot) /
        (early.groupby(type_col, observed=True)[cnt_col].sum()  / early_tot)
    ).replace([np.inf, -np.inf], np.nan).dropna()
    return ratio.idxmax() if not ratio.empty else None

# ─────────────────────── metric builder ───────────────────────────────

def build_metrics(full_df: pd.DataFrame, win_df: pd.DataFrame) -> dict:
    """Return a dict of all outreach metrics for one entity."""
    # Ensure `win_df` has the full column set (can be missing when an
    # empty / reduced frame slips through). This prevents KeyErrors.
    global df_empty  # template from window.head(0)
    if not set(df_empty.columns).issubset(win_df.columns):
        for col in df_empty.columns:
            if col not in win_df.columns:
                win_df[col] = pd.Series(dtype=df_empty[col].dtype)

    out: dict[str, object] = {}
    first = full_df.iloc[0]

    for col in (
        "contact_email", "account_id", "account_name", "tier_2025",
        "parent_account_id", "parent_account",
        "account_owner", "bda_owner", "cs_owner",
    ):
        if col in full_df.columns:
            out[col] = first[col]

    # ————————— core counts —————————
    out["facilities"]             = win_df["handler_id"].nunique()
    out["evaluations_5y"]         = win_df["eval_pk"].nunique()
    out["evals_with_viol_5y"]     = win_df.loc[win_df["is_violation"], "eval_pk"].nunique()
    out["violations_5y"]          = win_df["viol_pk"].nunique()
    out["enforcement_actions_5y"] = win_df["enf_pk"].nunique()
    out["total_penalties_5y"]     = round(win_df["penalty_amt"].sum(), 2)

    # ————————— lifetime extras —————————
    out["open_violations"]      = full_df["is_open_violation"].sum()
    out["last_eval_date"]       = int(full_df["eval_date_i"].dropna().max())  if full_df["eval_date_i"].notna().any() else None
    out["last_violation_date"]  = int(full_df["viol_date_i"].dropna().max()) if full_df["viol_date_i"].notna().any() else None
    out["last_penalty_date"]    = int(full_df["enf_date_i"].dropna().max())  if full_df["enf_date_i"].notna().any() else None

    out["eval_hit_rate_%"] = (
        round(100 * out["evals_with_viol_5y"] / out["evaluations_5y"], 1)
        if out["evaluations_5y"] else 0.0
    )

    # —— eval type with highest hit-rate ——
    if out["evaluations_5y"]:
        tmp = (
            win_df.groupby("eval_type_desc", observed=True)
                  .agg(evals=("eval_pk", "nunique"),
                       hits=("is_violation", "sum"))
        )
        tmp["hit_rate"] = 100 * tmp["hits"] / tmp["evals"]
        top = tmp.loc[tmp["hit_rate"].idxmax()]
        out["eval_type_max_hit"]        = top.name
        out["eval_type_max_hit_rate_%"] = round(top.hit_rate, 1)
    else:
        out["eval_type_max_hit"] = out["eval_type_max_hit_rate_%"] = None

    # —— eval trend / spike ——
    yr_eval = win_df.groupby("eval_year", observed=True).size()
    if not yr_eval.empty:
        out["eval_trend_slope_%"] = slope_pct(yr_eval.index.to_series(), yr_eval.values)
        spy = spike_year(yr_eval.index.to_series(), yr_eval.values)
        out["eval_spike_year"] = spy
        out["eval_spike_type"] = (
            win_df.loc[win_df["eval_year"] == spy, "eval_type_desc"].mode().iat[0]
            if spy else None
        )
        out["eval_type_to_watch"] = emerging_type(
            win_df.groupby(["eval_year", "eval_type_desc"], observed=True).size()
                  .reset_index(name="cnt")
                  .rename(columns={"eval_year": "year",
                                   "eval_type_desc": "type"}),
            current_year - 1, "type", "cnt"
        )
    else:
        out["eval_trend_slope_%"] = 0
        out["eval_spike_year"] = out["eval_spike_type"] = out["eval_type_to_watch"] = None

    # —— violation trend / spike ——
    yr_viol = win_df.groupby("viol_year", observed=True).size()
    if not yr_viol.empty:
        out["viol_trend_slope_%"] = slope_pct(yr_viol.index.to_series(), yr_viol.values)
        spy = spike_year(yr_viol.index.to_series(), yr_viol.values)
        out["spike_year"] = spy
        out["spike_violation_type"] = (
            win_df.loc[win_df["viol_year"] == spy, "viol_short_desc"].mode().iat[0]
            if spy else None
        )
        out["type_to_watch"] = emerging_type(
            win_df.groupby(["viol_year", "viol_short_desc"], observed=True).size()
                  .reset_index(name="cnt")
                  .rename(columns={"viol_year": "year",
                                   "viol_short_desc": "type"}),
            current_year - 1, "type", "cnt"
        )
    else:
        out["viol_trend_slope_%"] = 0
        out["spike_year"] = out["spike_violation_type"] = out["type_to_watch"] = None

    # —— largest penalty in window ——
    if win_df["penalty_amt"].notna().any():
        idx = win_df["penalty_amt"].idxmax()
        out["enf_type_largest_penalty"] = win_df.at[idx, "enf_type_desc"]
        out["largest_penalty_amt"]      = round(win_df.at[idx, "penalty_amt"], 2)
    else:
        out["enf_type_largest_penalty"] = None
        out["largest_penalty_amt"]      = 0.0

    # —— average days-to-resolve ——
    d = win_df["days_to_resolve"].dropna()
    out["avg_days_to_resolve"] = int(d.mean()) if not d.empty else None

    return out
# ───────────────────────────── build summaries ─────────────────────────────
print("📊  Building contact metrics …", flush=True)
contact_rows = [
    build_metrics(full_df,
                  win_contact_groups.get((email, acc), df_empty))
    for (email, acc), full_df
    in base.groupby(["contact_email", "account_id"], dropna=False, observed=True)
]
contact_df = pd.DataFrame(contact_rows)

print("📊  Building account metrics …", flush=True)
account_rows = [
    build_metrics(full_df, win_account_groups.get(acc, df_empty))
    for acc, full_df in base.groupby("account_id", observed=True)
]
account_df = pd.DataFrame(account_rows).drop(columns=["contact_email"],
                                             errors="ignore")

# ───────────────────────────── write CSVs ─────────────────────────────
contact_path = OUTPUT_DIR / "contact_compliance_summary.csv"
account_path = OUTPUT_DIR / "account_compliance_summary.csv"

print(f"💾  {contact_path}", flush=True)
contact_df.to_csv(contact_path, index=False)

print(f"💾  {account_path}", flush=True)
account_df.to_csv(account_path, index=False)

print("✅  Done.", flush=True)
