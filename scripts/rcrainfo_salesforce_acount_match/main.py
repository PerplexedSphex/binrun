import sys
import os
import pandas as pd
import tldextract
from pathlib import Path
from dotenv import load_dotenv
import duckdb
import re

def resolve_path(p):
    p = Path(p)
    if p.is_absolute():
        return p
    # Project root is two levels up from this script (scripts/rcrainfo_salesforce_acount_match/)
    return Path(__file__).resolve().parents[2] / p

def main():
    # Load .env if present
    SCRIPT_DIR = Path(__file__).parent.resolve()
    load_dotenv(SCRIPT_DIR / ".env")

    # Input/output paths from env or default, always resolve relative to project root
    SRC = resolve_path(os.getenv("SRC", "store/script_input_files/salesforce-account-domains.csv"))
    OUT = resolve_path(os.getenv("OUT", "store/script_output_files/account_domain_rows.csv"))
    BAD_DOMAINS_PATH = resolve_path(os.getenv("BAD_DOMAINS", "store/script_input_files/MASTER - Account Mapping with Domains - Generic_Bad Domains.csv"))

    # ---------------------------------------------------------------------------
    # 1. Load + rename the columns you want to keep
    # ---------------------------------------------------------------------------
    KEEP = {
        "Account ID18":        "account_id",
        "Account Name":        "account_name",
        "Account Status (Formulaic)": "account_status",
        "RY2025 Account Tier": "tier_2025",
        "Parent Account ID":   "parent_account_id",
        "Parent Account":     "parent_account",
        "RY2025 Highest Tier": "highest_tier",
        "Account Owner":       "account_owner",
        "BDA Owner":           "bda_owner",
        "CS Owner":            "cs_owner",
        "Email":               "email",
    }
    df = (
        pd.read_csv(SRC, dtype=str)
          .fillna("")
          [KEEP.keys()]
          .rename(columns=KEEP)
    )

    # ---------------------------------------------------------------------------
    # 2. Collect every column that could hide a domain or e-mail
    # ---------------------------------------------------------------------------
    DOMAIN_COLS = [
        "Website", "FRS Domain", "Domain for Salesloft",
        "Extra Domains", "Other Domains (ZoomInfo)",
        "SalesLoft Domain", "Email",
    ]

    raw = pd.read_csv(SRC, dtype=str).fillna("")

    # Load bad domains to filter out
    if BAD_DOMAINS_PATH.exists():
        bad_domains = set(pd.read_csv(BAD_DOMAINS_PATH, dtype=str, header=None)[0].str.strip().str.lower())
    else:
        bad_domains = set()

    def clean_domains(cell: str) -> list[str]:
        out = []
        for piece in (
            cell.replace(";", ",").replace("|", ",").replace(" ", ",").split(",")
        ):
            piece = piece.strip()
            if not piece:
                continue
            if "@" in piece:          # keep domain part of email
                piece = piece.split("@", 1)[-1]
            piece = (
                piece.replace("https://", "")
                     .replace("http://", "")
                     .split("/")[0]
                     .split("?")[0]
            )
            rd = tldextract.extract(piece).top_domain_under_public_suffix
            if rd:
                out.append(rd.lower())
        return out

    domain_lists = (
        raw[DOMAIN_COLS]
        .applymap(clean_domains)                      # lists in each cell
        .apply(lambda row: sum(row, []), axis=1)      # concat lists per row
    )

    # Filter out bad domains from domain_lists
    domain_lists = domain_lists.apply(lambda domains: [d for d in domains if d not in bad_domains])

    # ---------------------------------------------------------------------------
    # 3. Explode to one row per (account, domain)
    # ---------------------------------------------------------------------------
    df_domains = (
        df.assign(domain=domain_lists)
          .explode("domain")
          .query("domain != ''")                      # drop blanks
    )

    # Add email_domain column for matching
    def extract_email_domain(email):
        if not email or '@' not in email:
            return ''
        return email.split('@', 1)[-1].lower().strip()
    df_domains['email_domain'] = df_domains['email'].apply(extract_email_domain)

    # Group by all columns except 'email', 'email_domain', and 'domain', then group by domain
    group_cols = [col for col in df_domains.columns if col not in ("email", "email_domain", "domain")]
    def count_matching_emails(subdf):
        dom = subdf['domain'].iloc[0]
        emails = set(
            e for e, ed in zip(subdf['email'], subdf['email_domain'])
            if e and ed == dom
        )
        return pd.Series({
            "distinct_email_count": len(emails)
        })
    df_grouped = (
        df_domains.groupby(group_cols + ["domain"], dropna=False)
        .apply(count_matching_emails)
        .reset_index()
    )

    OUT.parent.mkdir(parents=True, exist_ok=True)
    OUT_FULL = OUT
    OUT_TOP = OUT.parent / "account_top_domain.csv"
    df_grouped.to_csv(OUT_FULL, index=False)
    print(f"✅ wrote {len(df_grouped):,} account-domain rows → {OUT_FULL}")

    # Add top domain file
    df_top = df_grouped.groupby('domain', dropna=False).apply(lambda subdf: pd.Series({
        "top_domain": subdf['domain'].iloc[0],
        "top_domain_count": subdf['distinct_email_count'].iloc[0]
    })).reset_index()
    df_top.to_csv(OUT_TOP, index=False)
    print(f"✅ wrote {len(df_top):,} top domains → {OUT_TOP}")

    # --- Write to DuckDB: account_matched_facilities ---
    DB_PATH = Path(os.getenv("DB_PATH", SCRIPT_DIR.parent.parent / "store" / "db" / "rcrainfo.duckdb"))
    print(f"🦆 Connecting to DuckDB at {DB_PATH}")
    with duckdb.connect(database=DB_PATH, read_only=False) as con:
        print("📤 Writing account_domain_rows table to DuckDB...")
        con.execute("DROP TABLE IF EXISTS account_domain_rows")
        con.execute(f"CREATE TABLE account_domain_rows AS SELECT * FROM read_csv_auto('{str(OUT_FULL)}', HEADER=TRUE)")
        print("🔗 Joining account_domain_rows with hd_handler on domain...")
        # Explicitly list account fields in order
        account_fields = [
            "account_id", "account_name", "account_status", "tier_2025", "parent_account_id", "parent_account",
            "highest_tier", "account_owner", "bda_owner", "cs_owner", "domain", "distinct_email_count"
        ]
        handler_cols = [col for col in con.execute("DESCRIBE hd_handler").fetchdf()['column_name'].tolist() if col not in account_fields]
        all_cols = ', '.join([f"a.{col}" for col in account_fields] + [f"h.{col}" for col in handler_cols] + ["split_part(h.contact_email_address, '@', 2) AS contact_email_domain"])
        join_sql = f'''
            CREATE OR REPLACE TABLE account_matched_facilities AS
            SELECT {all_cols}
            FROM account_domain_rows a
            JOIN hd_handler h
              ON lower(a.domain) = lower(split_part(h.contact_email_address, '@', 2))
             AND h.current_record = 'Y'
        '''
        print("📝 Writing account_matched_facilities table to DuckDB...")
        con.execute(join_sql)
        print("🔢 Counting rows in account_matched_facilities...")
        row_count = con.execute("SELECT COUNT(*) FROM account_matched_facilities").fetchone()[0]
    print(f"✅ wrote {row_count:,} rows to DuckDB table account_matched_facilities at {DB_PATH}")

if __name__ == "__main__":
    main()
