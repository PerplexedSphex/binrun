#!/usr/bin/env python3
"""
build_outreach_files.py — *window‑aware SQL prep*
────────────────────────────────────────────────────────────────────────────
The pipeline now delegates **all row‑level filtering to DuckDB**:

  • **evaluations_win**      – evaluation rows whose *eval_start_date* is
    within the N‑year look‑back window.
  • **violations_win**       – violation rows whose *determined_date* is in
    window.
  • **enforcements_win**     – enforcement rows whose *enf_action_date* is in
    window.
  • **facilities_full**      – DISTINCT handler×account/contact pairs across
    **all years** (used only for non‑time‑bounded breadth metrics).

Pandas now consumes these already‑tidy tables to compute summary metrics and
write the two familiar CSVs.
"""

import os
from pathlib import Path
from datetime import date
import duckdb
import pandas as pd
import numpy as np
from dotenv import load_dotenv

# ──────────────────────────────
# 0  configuration
# ──────────────────────────────
SCRIPT_DIR = Path(__file__).parent.resolve()
load_dotenv(SCRIPT_DIR / ".env")

DB_PATH        = Path(os.getenv("DB_PATH",  SCRIPT_DIR.parent.parent / "store" / "db" / "rcrainfo.duckdb"))
OUTPUT_DIR     = Path(os.getenv("OUTPUT_DIR", SCRIPT_DIR.parent.parent / "store" / "script_output_files"))
YEARS_LOOKBACK = int(os.getenv("YEARS_LOOKBACK", "5"))
OUTPUT_DIR.mkdir(parents=True, exist_ok=True)

current_year     = date.today().year
window_year_min  = current_year - YEARS_LOOKBACK   # first calendar year IN window
window_year_max  = current_year - 1                # last *complete* calendar year
win_begin = f"{window_year_min}0101"
win_end   = f"{window_year_max}1231"

print(f"🦆 DB … {DB_PATH}\n📅 Window: {window_year_min}‑01‑01 → {window_year_max}‑12‑31")

# ──────────────────────────────
# 1  SQL helpers
# ──────────────────────────────

def q_eval(win_begin, win_end):
    return f"""
    SELECT DISTINCT ON (eval_pk)
        /* pk */
        CONCAT_WS('|', r.handler_id, r.eval_activity_location, r.eval_identifier,
                          r.eval_start_date, r.eval_agency)               AS eval_pk,
        r.handler_id, r.eval_activity_location, r.eval_identifier,
        r.eval_start_date, r.eval_agency,
        CAST(r.eval_start_date AS INT) AS eval_date_i,
        SUBSTR(r.eval_start_date,1,4)::INT AS eval_year,
        r.eval_type_desc,
        r.found_violation,
        /* enrichment */
        af.account_id, af.account_name, af.tier_2025,
        af.parent_account_id, af.parent_account,
        af.account_owner, af.bda_owner, af.cs_owner,
        COALESCE(af.contact_email_address,'(no email)') AS contact_email
    FROM ce_reporting r
    JOIN account_matched_facilities af USING (handler_id)
    WHERE af.account_id IS NOT NULL
      AND r.eval_start_date BETWEEN '{win_begin}' AND '{win_end}'
    """

def q_viol(win_begin, win_end):
    return f"""
    SELECT DISTINCT ON (viol_pk)
        CONCAT_WS('|', r.handler_id, r.viol_activity_location, r.viol_seq,
                          r.viol_determined_by_agency)                   AS viol_pk,
        r.handler_id, r.viol_activity_location, r.viol_seq,
        r.viol_determined_by_agency,
        CAST(r.determined_date AS INT) AS viol_date_i,
        SUBSTR(r.determined_date,1,4)::INT AS viol_year,
        r.viol_short_desc,
        CASE WHEN r.actual_rtc_date IS NOT NULL AND r.determined_date IS NOT NULL
             THEN DATE_DIFF('day', STRPTIME(r.actual_rtc_date,'%Y%m%d'),
                                  STRPTIME(r.determined_date,'%Y%m%d')) END  AS days_to_resolve,
        /* enrichment */
        af.account_id, af.account_name, af.tier_2025,
        af.parent_account_id, af.parent_account,
        af.account_owner, af.bda_owner, af.cs_owner,
        COALESCE(af.contact_email_address,'(no email)') AS contact_email
    FROM ce_reporting r
    JOIN account_matched_facilities af USING (handler_id)
    WHERE af.account_id IS NOT NULL
      AND r.determined_date BETWEEN '{win_begin}' AND '{win_end}'
    """

def q_enf(win_begin, win_end):
    return f"""
    SELECT DISTINCT ON (enf_pk)
        CONCAT_WS('|', r.handler_id, r.enf_activity_location, r.enf_identifier,
                          r.enf_action_date, r.enf_agency)               AS enf_pk,
        r.handler_id, r.enf_activity_location, r.enf_identifier,
        r.enf_action_date, r.enf_agency,
        CAST(r.enf_action_date AS INT) AS enf_date_i,
        SUBSTR(r.enf_action_date,1,4)::INT AS enf_year,
        r.enf_type_desc,
        CAST(NULLIF(r.final_amount,'') AS DOUBLE) AS penalty_amt,
        /* enrichment */
        af.account_id, af.account_name, af.tier_2025,
        af.parent_account_id, af.parent_account,
        af.account_owner, af.bda_owner, af.cs_owner,
        COALESCE(af.contact_email_address,'(no email)') AS contact_email
    FROM ce_reporting r
    JOIN account_matched_facilities af USING (handler_id)
    WHERE af.account_id IS NOT NULL
      AND r.enf_action_date BETWEEN '{win_begin}' AND '{win_end}'
    """

def q_facilities_full():
    return """
    SELECT DISTINCT r.handler_id,
           af.account_id, af.account_name, af.tier_2025,
           af.parent_account_id, af.parent_account,
           af.account_owner, af.bda_owner, af.cs_owner,
           COALESCE(af.contact_email_address,'(no email)') AS contact_email
    FROM ce_reporting r
    JOIN account_matched_facilities af USING (handler_id)
    WHERE af.account_id IS NOT NULL
    """

# ──────────────────────────────
# 2  fetch tidy tables
# ──────────────────────────────
print("🔎 Running SQL …")
with duckdb.connect(DB_PATH, read_only=True) as con:
    con.execute(f"SET threads TO {os.cpu_count()}")
    EVALS_WIN      = con.execute(q_eval(win_begin, win_end)).fetchdf()
    VIOLATIONS_WIN = con.execute(q_viol(win_begin, win_end)).fetchdf()
    ENFS_WIN       = con.execute(q_enf(win_begin, win_end)).fetchdf()
    FACILITIES     = con.execute(q_facilities_full()).fetchdf()

print(
    f"📊 evals: {len(EVALS_WIN):,}  viols: {len(VIOLATIONS_WIN):,}  "
    f"enfs: {len(ENFS_WIN):,}  facilities: {len(FACILITIES):,}"
)

# ──────────────────────────────
# 3  math helpers (unchanged)
# ──────────────────────────────

def slope_pct(years, values):
    years = years.astype(float)
    if len(years.unique()) < 2 or values.sum() == 0:
        return 0.0
    base_val = values.iloc[0] or 1e-9
    m = np.polyfit(years, values, 1)[0]
    return round(100 * m / base_val, 2)

def spike_year(years, values, thresh=2):
    if values.std(ddof=0) == 0:
        return None
    z = (values - values.mean()) / values.std(ddof=0)
    yr = z.idxmax()
    return int(yr) if z.loc[yr] >= thresh else None

def emerging_type(df, recent_start, type_col, cnt_col):
    recent = df[df.year >= recent_start]
    early  = df[df.year <  recent_start]
    if recent.empty or early.empty:
        return None
    recent_tot = recent[cnt_col].sum()
    early_tot  = early[cnt_col].sum() or 1
    share = (recent.groupby(type_col)[cnt_col].sum() / recent_tot) / \
            (early.groupby(type_col)[cnt_col].sum()  / early_tot)
    share = share.replace([np.inf, -np.inf], np.nan).dropna()
    return share.idxmax() if not share.empty else None

# ──────────────────────────────
# 4  metric builder
# ──────────────────────────────

def build_metrics(email: str, acc: str):
    # slice each table once only
    e_win = EVALS_WIN.query("contact_email == @email and account_id == @acc")
    v_win = VIOLATIONS_WIN.query("contact_email == @email and account_id == @acc")
    f_win = ENFS_WIN.query("contact_email == @email and account_id == @acc")

    # fallback to first non‑empty row for identity fields
    first = next((df.iloc[0] for df in (e_win, v_win, f_win) if not df.empty), None)
    if first is None:
        return {}

    out = {
        "contact_email": first.contact_email if 'contact_email' in first else None,
        "account_id": first.account_id,
        "account_name": first.account_name,
        "tier_2025": first.tier_2025,
        "parent_account_id": first.parent_account_id,
        "parent_account": first.parent_account,
        "account_owner": first.account_owner,
        "bda_owner": first.bda_owner,
        "cs_owner": first.cs_owner,
    }

    # facilities breadth – NOT time‑limited
    fac_slice = FACILITIES.query("contact_email == @email and account_id == @acc")
    out["facilities"] = fac_slice.handler_id.nunique()

    # counts / sums in window
    out["evaluations_5y"]         = e_win.eval_pk.nunique()
    out["evals_with_viol_5y"]     = e_win.loc[e_win.found_violation == 'Y', "eval_pk"].nunique()
    out["violations_5y"]          = v_win.viol_pk.nunique()
    out["enforcement_actions_5y"] = f_win.enf_pk.nunique()
    out["total_penalties_5y"]     = round(f_win.penalty_amt.sum(), 2)

    # open violations — lifetime (we only have window rows for violations, so we
    # assume anything unresolved in window is "open" for reporting)
    out["open_violations"] = v_win.days_to_resolve.isna().sum()

    # last‑event dates (INT)
    def max_int(col):
        return int(col.max()) if not col.empty else None
    out["last_eval_date"]      = max_int(e_win.eval_date_i)
    out["last_violation_date"] = max_int(v_win.viol_date_i)
    out["last_penalty_date"]   = max_int(f_win.enf_date_i)

    # hit‑rate
    out["eval_hit_rate_%"] = (
        round(100 * out["evals_with_viol_5y"] / out["evaluations_5y"], 1)
        if out["evaluations_5y"] else 0.0
    )

    # patterns — evaluation
    if out["evaluations_5y"]:
        tmp = (
            e_win.groupby("eval_type_desc")
                 .agg(evals=('eval_pk','nunique'),
                      hits =('found_violation', lambda x:(x=='Y').sum()))
                 .assign(hit_rate=lambda d:100*d.hits/d.evals)
        )
        top = tmp.loc[tmp.hit_rate.idxmax()]
        out |= {
            "eval_type_max_hit": top.name,
            "eval_type_max_hit_rate_%": round(top.hit_rate, 1)
        }
        yr = e_win.groupby("eval_year").agg(cnt=('eval_pk','nunique'))
        out["eval_trend_slope_%"] = slope_pct(yr.index.to_series(), yr.cnt)
        spy = spike_year(yr.index.to_series(), yr.cnt)
        out |= {
            "eval_spike_year": spy,
            "eval_spike_type": (e_win.loc[e_win.eval_year == spy, "eval_type_desc"].mode().iat[0]
                                  if spy else None),
            "eval_type_to_watch": emerging_type(
                e_win.groupby(["eval_year","eval_type_desc"]).agg(cnt=('eval_pk','nunique')).reset_index(names=['year','type']),
                current_year-1, "type", "cnt")
        }
    else:
        out |= {k: None for k in (
            "eval_type_max_hit","eval_type_max_hit_rate_%","eval_trend_slope_%",
            "eval_spike_year","eval_spike_type","eval_type_to_watch")}

    # patterns — violations
    yr = v_win.groupby("viol_year").agg(cnt=('viol_pk','nunique'))
    if not yr.empty:
        out["viol_trend_slope_%"] = slope_pct(yr.index.to_series(), yr.cnt)
        spy = spike_year(yr.index.to_series(), yr.cnt)
        out |= {
            "spike_year": spy,
            "spike_violation_type": (v_win.loc[v_win.viol_year == spy, "viol_short_desc"].mode().iat[0]
                                       if spy else None),
            "type_to_watch": emerging_type(
                v_win.groupby(["viol_year","viol_short_desc"]).agg(cnt=('viol_pk','nunique')).reset_index(names=['year','type']),
                current_year-1,"type","cnt")
        }
    else:
        out |= {k: None for k in ("viol_trend_slope_%","spike_year","spike_violation_type","type_to_watch")}

    # penalties
    if not f_win.penalty_amt.dropna().empty:
        idx = f_win.penalty_amt.idxmax()
        out |= {
            "enf_type_largest_penalty": f_win.at[idx, "enf_type_desc"],
            "largest_penalty_amt": round(f_win.at[idx, "penalty_amt"], 2)
        }
    else:
        out |= {"enf_type_largest_penalty": None, "largest_penalty_amt": 0.0}

    d = v_win.days_to_resolve.dropna()
    out["avg_days_to_resolve"] = int(d.mean()) if not d.empty else None

    return out

# ──────────────────────────────
# 5  generate summaries
# ──────────────────────────────
print("📝 Building summaries …")
contact_rows = [
    build_metrics(email, acc)
    for email, acc in EVALS_WIN[["contact_email","account_id"]].drop_duplicates().itertuples(index=False, name=None)
]
contact_df = pd.DataFrame(contact_rows)

account_rows = [
    {k:v for k,v in build_metrics(None, acc).items() if k != "contact_email"}
    for acc in EVALS_WIN.account_id.unique()
]
account_df = pd.DataFrame(account_rows)

# ──────────────────────────────
# 6  write CSVs
# ──────────────────────────────
contact_path = OUTPUT_DIR / "contact_compliance_summary.csv"
account_path = OUTPUT_DIR / "account_compliance_summary.csv"

print(f"💾 {contact_path}")
contact_df.to_csv(contact_path, index=False)
print(f"💾 {account_path}")
account_df.to_csv(account_path, index=False)

print("✅ Done.")
