# combined_search.py

import argparse
import os
import re
import shutil
import sys
import time
from datetime import datetime
from pathlib import Path
from typing import Dict, Any, List, Set
import json

import duckdb
import pandas as pd
from tqdm import tqdm

# -------------------------------------------------------------------
# Settings / Config (replace with your actual config system)
# -------------------------------------------------------------------
class Settings:
    # Ensure DATA_ROOT is a Path object, not str
    DATA_ROOT = Path("/Users/sam/gonads-playground/scripts/download_rcrainfo/data")
    PROCESSED_DATA_DIR = DATA_ROOT / "processed"
    RESULTS_ROOT = Path.cwd() / Path("results")

settings = Settings()

# -------------------------------------------------------------------
# Utility: progress tracking
# -------------------------------------------------------------------
class SearchProgress:
    """
    Shows a progress bar for search operations.
    """
    def __init__(self, total_accounts: int, source: str):
        self.start_time = time.time()
        self.source = source
        self.pbar = tqdm(
            total=total_accounts,
            desc=f"{source} Search Progress",
            bar_format='{desc}: {percentage:3.0f}%|{bar}| {n_fmt}/{total_fmt} [{elapsed}<{remaining}]'
        )
        
    def update(self, account: Dict[str, Any]):
        """Update the progress bar with current account info."""
        elapsed = time.time() - self.start_time
        hh = int(elapsed // 3600)
        mm = int((elapsed % 3600) // 60)
        ss = int(elapsed % 60)
        self.pbar.set_description(
            f"{self.source} Search [{hh:02d}:{mm:02d}:{ss:02d}] - {account['account_name']}"
        )
        self.pbar.update(1)
    
    def close(self):
        self.pbar.close()

# -------------------------------------------------------------------
# Utility: domain cleaning and validation (for search term preprocessing)
# -------------------------------------------------------------------
def clean_domain(domain: str) -> str:
    """Remove protocols, 'www.', trailing slashes, '?' queries, etc."""
    domain = re.sub(r'^https?://', '', domain)
    domain = re.sub(r'^www\.', '', domain)
    domain = domain.split('?')[0].rstrip('/').strip()
    return domain

def is_valid_domain(domain: str) -> bool:
    """Return True if `domain` is a valid domain to keep, else False."""
    domain = domain.lower().strip()
    exclude_patterns = [
        'yelp.com', 'cm', '.pdf', '.doc', '.txt', '.html',
        'linkedin.com','facebook.com','twitter.com','instagram.com','indeed.com',
        'glassdoor.com','monster.com','careerbuilder.com',
        'gmail.com','hotmail.com','yahoo.com','outlook.com','aol.com','icloud.com',
        'me.com','msn.com','live.com','mail.com','protonmail.com','yandex.com','zoho.com'
    ]
    if any(pattern in domain for pattern in exclude_patterns):
        return False
    if re.search(r'\d+$', domain):  # ends in digits?
        return False
    if not re.match(r'^[a-z0-9][a-z0-9-.]+(\.)[a-z]{2,}$', domain):
        return False
    return True

def extract_email_domain(email: str) -> str:
    """Extract part after '@' if present."""
    return email.split('@')[1].lower() if '@' in email else email.lower()

# -------------------------------------------------------------------
# Preprocessing logic for "customer account data -> search terms"
# -------------------------------------------------------------------
def clean_name_terms(name: str) -> str:
    """Generate multiple name variants to catch apostrophes, etc."""
    name = name.replace('’', "'").replace('‘', "'")
    variants = {
        name,
        name.replace("'", ""),
        name.replace("'", " "),
        re.sub(r"'s\b", "", name),
        re.sub(r"'s\b", "s", name),
        re.sub(r"s'\b", "s", name)
    }
    variants = {v.strip() for v in variants if v.strip()}
    return ';'.join(sorted(variants))

def process_domains(row: pd.Series) -> Set[str]:
    """Collect all domain-related fields from a row, return unique valid domains."""
    domains = set()
    fields = [
        'Website','FRS Domain','Domain for Salesloft','Extra Domains','SalesLoft Domain','Other Domains (ZoomInfo)'
    ]
    for f in fields:
        val = row.get(f)
        if pd.notna(val) and val:
            # Might be multiple domains in a single field
            for d in str(val).split(','):
                d_clean = clean_domain(d.strip().lower())
                if is_valid_domain(d_clean):
                    domains.add(d_clean)
    # Also check if there's an 'Email' column
    if pd.notna(row.get('Email')):
        email_dom = extract_email_domain(str(row['Email']))
        if is_valid_domain(email_dom):
            domains.add(email_dom)
    return domains

def process_search_terms(input_path: str, output_path: str):
    """
    Convert raw customer account CSV into 'search_terms' with
    columns: [account_id, account_name, name_terms, email_terms].
    """
    df = pd.read_csv(input_path)
    processed_data = []
    seen_accounts = set()

    # Decide if columns are like [Account ID18, Account Name] or [account_id, account_name]
    has_classic_cols = 'Account Name' in df.columns
    for _, row in df.iterrows():
        if has_classic_cols:
            acct_id = row['Account ID18']
            acct_name = row['Account Name']
        else:
            acct_id = row['account_id']
            acct_name = row['account_name']

        if acct_id in seen_accounts:
            continue
        seen_accounts.add(acct_id)

        # gather domains
        domains = process_domains(row)
        processed_data.append({
            'account_id': acct_id,
            'account_name': acct_name,
            'name_terms': clean_name_terms(str(acct_name)),
            'email_terms': ';'.join(sorted(domains)) if domains else ''
        })

    out_df = pd.DataFrame(processed_data)
    os.makedirs(Path(output_path).parent, exist_ok=True)
    out_df.to_csv(output_path, index=False)
    print(f"Processed {len(processed_data)} unique accounts --> {output_path}")

# -------------------------------------------------------------------
# RCRASearcher logic
# (We modified it to delete only rows for the specific account IDs!)
# -------------------------------------------------------------------
class RCRASearcher:
    def __init__(self, input_file: Path, output_dir: Path):
        print("Initializing RCRASearcher...")
        self.db_path = settings.PROCESSED_DATA_DIR / "rcra" / "rcrainfo.duckdb"
        self.con = duckdb.connect(str(self.db_path))
        
        # (Re)create handler_owner_operator view
        self.con.execute("""
            CREATE OR REPLACE VIEW handler_owner_operator AS
            WITH owner_operator_agg AS (
                SELECT 
                    handler_id,
                    activity_location,
                    source_type,
                    seq_number,
                    STRING_AGG(CASE WHEN owner_operator_indicator='CO'
                             THEN owner_operator_name END, '; ') AS owner_names,
                    STRING_AGG(CASE WHEN owner_operator_indicator='CP'
                             THEN owner_operator_name END, '; ') AS operator_names,
                    STRING_AGG(CASE WHEN owner_operator_indicator='CO'
                             THEN email END, '; ') AS owner_emails,
                    STRING_AGG(CASE WHEN owner_operator_indicator='CP'
                             THEN email END, '; ') AS operator_emails,
                    STRING_AGG(CASE WHEN owner_operator_indicator='CO' AND email IS NOT NULL
                             THEN regexp_extract(email, '@(.+)$',1) END, '; ') AS owner_email_domains,
                    STRING_AGG(CASE WHEN owner_operator_indicator='CP' AND email IS NOT NULL
                             THEN regexp_extract(email, '@(.+)$',1) END, '; ') AS operator_email_domains
                FROM hd_owner_operator
                GROUP BY handler_id, activity_location, source_type, seq_number
            )
            SELECT DISTINCT
                h.handler_id,
                h.activity_location,
                h.source_type,
                h.seq_number,
                h.receive_date,
                h.handler_name,
                h.location_street_no,
                h.location_street1,
                h.location_street2,
                h.location_city,
                h.location_state,
                h.location_zip,
                h.location_country,
                h.county_code,
                h.contact_first_name,
                h.contact_middle_initial,
                h.contact_last_name,
                h.contact_phone,
                h.contact_phone_ext,
                h.contact_fax,
                h.contact_email_address,
                CASE WHEN h.contact_email_address IS NOT NULL 
                     THEN regexp_extract(h.contact_email_address,'@(.+)$',1)
                     END as contact_email_domain,
                h.contact_title,
                h.fed_waste_generator,
                h.state_waste_generator,
                h.short_term_generator,
                h.current_record,
                o.owner_names, o.operator_names,
                o.owner_emails, o.operator_emails,
                o.owner_email_domains, o.operator_email_domains
            FROM hd_handler h
            LEFT JOIN owner_operator_agg o
            ON h.handler_id = o.handler_id
             AND h.activity_location = o.activity_location
             AND h.source_type = o.source_type
             AND h.seq_number = o.seq_number
            WHERE h.current_record = 'Y'
        """)

        # (Re)create account_matched_facilities table if needed
        self.con.execute("""
            CREATE TABLE IF NOT EXISTS account_matched_facilities (
                account_id VARCHAR,
                account_name VARCHAR,
                handler_id VARCHAR,
                activity_location VARCHAR,
                source_type VARCHAR,
                seq_number VARCHAR,
                receive_date VARCHAR,
                handler_name VARCHAR,
                location_street_no VARCHAR,
                location_street1 VARCHAR,
                location_street2 VARCHAR,
                location_city VARCHAR,
                location_state VARCHAR,
                location_zip VARCHAR,
                location_country VARCHAR,
                county_code VARCHAR,
                contact_first_name VARCHAR,
                contact_middle_initial VARCHAR,
                contact_last_name VARCHAR,
                contact_phone VARCHAR,
                contact_phone_ext VARCHAR,
                contact_fax VARCHAR,
                contact_email_address VARCHAR,
                contact_email_domain VARCHAR,
                contact_title VARCHAR,
                fed_waste_generator VARCHAR,
                state_waste_generator VARCHAR,
                short_term_generator VARCHAR,
                current_record VARCHAR,
                owner_names VARCHAR,
                operator_names VARCHAR,
                owner_emails VARCHAR,
                operator_emails VARCHAR,
                owner_email_domains VARCHAR,
                operator_email_domains VARCHAR,
                handler_name_match BOOLEAN NOT NULL DEFAULT FALSE,
                owner_name_match BOOLEAN NOT NULL DEFAULT FALSE,
                operator_name_match BOOLEAN NOT NULL DEFAULT FALSE,
                contact_email_match BOOLEAN NOT NULL DEFAULT FALSE,
                owner_email_match BOOLEAN NOT NULL DEFAULT FALSE,
                operator_email_match BOOLEAN NOT NULL DEFAULT FALSE
            )
        """)

        # Load search terms
        self.search_terms_file = input_file
        print(f"Loading search terms from {self.search_terms_file}...")
        self.search_terms = pd.read_csv(self.search_terms_file)
        
        self.results_dir = output_dir
        self.results_dir.mkdir(parents=True, exist_ok=True)
        self.progress = SearchProgress(len(self.search_terms), "RCRA")

    def _create_word_pattern(self, terms: List[str]) -> str:
        # split by semicolon, add word boundaries, OR them together
        tokens = []
        for t in terms:
            for sub in t.split(';'):
                cleaned = sub.strip()
                if cleaned:
                    tokens.append(r'\b' + re.escape(cleaned.upper()) + r'\b')
        return '|'.join(tokens)

    def _delete_old_records_for_accounts(self):
        """
        Delete only the rows in account_matched_facilities for the accounts
        that are about to be searched (instead of deleting everything).
        """
        unique_ids = list(self.search_terms['account_id'].unique())
        if unique_ids:
            # Build SQL string: 'a1','a2','a3'
            in_clause = ','.join(f"'{uid}'" for uid in unique_ids)
            self.con.execute(
                f"DELETE FROM account_matched_facilities WHERE account_id IN ({in_clause})"
            )

    def search(self, account_id: str, account_name: str, name_terms: str, email_terms: str) -> Dict[str, Any]:
        self.progress.update({
            'account_id': account_id,
            'account_name': account_name
        })
        name_list = [x.strip() for x in name_terms.split(';') if x.strip()]
        email_list = [x.strip().lower() for x in email_terms.split(';') if x.strip()] if pd.notna(email_terms) else []

        # Build name & email patterns
        name_pattern = self._create_word_pattern(name_list).replace("'", "''")
        escaped_acct_name = account_name.replace("'", "''")

        # We'll match names using regex on [handler_name, owner_names, operator_names]
        conds_name = [
            f"COALESCE(regexp_matches(UPPER(handler_name), '({name_pattern})'), false)",
            f"COALESCE(regexp_matches(UPPER(owner_names), '({name_pattern})'), false)",
            f"COALESCE(regexp_matches(UPPER(operator_names), '({name_pattern})'), false)"
        ]
        # For email domain matching, do exact membership checks
        conds_email = []
        if email_list:
            domain_list_sql = ", ".join(f"'{dom}'" for dom in email_list)
            conds_email = [
                f"COALESCE(LOWER(contact_email_domain) IN ({domain_list_sql}), false)",
                f"COALESCE(LOWER(owner_email_domains) IN ({domain_list_sql}), false)",
                f"COALESCE(LOWER(operator_email_domains) IN ({domain_list_sql}), false)"
            ]

        try:
            # Build final SQL
            sql = f"""
            WITH matches AS (
              SELECT
                '{account_id}' AS account_id,
                '{escaped_acct_name}' AS account_name,
                h.*,
                {conds_name[0]} AS handler_name_match,
                {conds_name[1]} AS owner_name_match,
                {conds_name[2]} AS operator_name_match,
                {conds_email[0] if conds_email else 'FALSE'} AS contact_email_match,
                {conds_email[1] if conds_email else 'FALSE'} AS owner_email_match,
                {conds_email[2] if conds_email else 'FALSE'} AS operator_email_match
              FROM handler_owner_operator h
              WHERE ({" OR ".join(conds_name)})
                    {" OR " + " OR ".join(conds_email) if conds_email else ""}
            )
            INSERT INTO account_matched_facilities
            SELECT * FROM matches
            """
            self.con.execute(sql)

            # Gather stats for this account
            q_count = """
            SELECT COUNT(*) FROM account_matched_facilities 
            WHERE account_id=? AND account_name=?
            """
            total_matches = self.con.execute(q_count, [account_id, account_name]).fetchone()[0]

            def match_count(col):
                return self.con.execute(
                    f"{q_count} AND {col}", [account_id, account_name]
                ).fetchone()[0]

            return {
                'account_id': account_id,
                'account_name': account_name,
                'total_matches': total_matches,
                'handler_name_match': match_count('handler_name_match'),
                'owner_name_match': match_count('owner_name_match'),
                'operator_name_match': match_count('operator_name_match'),
                'contact_email_match': match_count('contact_email_match'),
                'owner_email_match': match_count('owner_email_match'),
                'operator_email_match': match_count('operator_email_match')
            }
        except Exception as e:
            print(f"Error searching for {account_name}: {e}")
            raise

    def run(self):
        print("Deleting old records for the relevant account_ids only...")
        self._delete_old_records_for_accounts()

        print("Searching RCRA data for each account...")
        results = []
        for _, row in self.search_terms.iterrows():
            res = self.search(
                account_id=row['account_id'],
                account_name=row['account_name'],
                name_terms=row['name_terms'],
                email_terms=row['email_terms']
            )
            results.append(res)

        self.progress.close()
        summary_df = pd.DataFrame(results)
        summary_df.to_csv(self.results_dir / "search_summary.csv", index=False)
        print(f"Search complete. Summary -> {self.results_dir / 'search_summary.csv'}")

# -------------------------------------------------------------------
# (Example) FRS Searcher
# This is assumed to be similar to RCRASearcher; adapt as needed.
# -------------------------------------------------------------------
class FRSSearcher:
    def __init__(self, input_file: Path, output_dir: Path):
        print("Initializing FRSSearcher...")
        # Example: if you have a different DB, change here:
        self.db_path = settings.PROCESSED_DATA_DIR / "frs" / "frs.duckdb"
        self.con = duckdb.connect(str(self.db_path))
        # Possibly create or replace relevant views/tables...
        self.con.execute("""
            CREATE TABLE IF NOT EXISTS account_matched_frs (
                account_id VARCHAR, account_name VARCHAR, facility_name VARCHAR,
                handler_name_match BOOLEAN,
                ...
            )
        """)

        self.search_terms_file = input_file
        self.search_terms = pd.read_csv(input_file)
        self.results_dir = output_dir
        self.results_dir.mkdir(parents=True, exist_ok=True)
        self.progress = SearchProgress(len(self.search_terms), "FRS")

    def _delete_old_records_for_accounts(self):
        """
        Only delete matching records for the accounts we are about to search.
        """
        unique_ids = list(self.search_terms['account_id'].unique())
        if unique_ids:
            in_clause = ','.join(f"'{uid}'" for uid in unique_ids)
            self.con.execute(f"DELETE FROM account_matched_frs WHERE account_id IN ({in_clause})")

    def run_search_for_account(self, account_id: str, account_name: str):
        """Example per-account search logic for FRS."""
        self.progress.update({'account_id': account_id, 'account_name': account_name})
        # do real searching logic here...
        self.con.execute(f"""
            INSERT INTO account_matched_frs SELECT
            '{account_id}' as account_id,
            '{account_name}' as account_name,
            facility_name,
            TRUE as handler_name_match
            -- etc.
            FROM frs_facilities
            WHERE facility_name ILIKE '%{account_name}%'
        """)

    def run(self):
        self._delete_old_records_for_accounts()
        for _, row in self.search_terms.iterrows():
            self.run_search_for_account(row['account_id'], row['account_name'])
        self.progress.close()

        # Save summary
        # you'd do something like:
        df = self.con.execute("SELECT account_id, account_name, COUNT(*) AS total_matches FROM account_matched_frs GROUP BY 1,2").df()
        df.to_csv(self.results_dir / "search_summary.csv", index=False)
        print(f"FRS search done -> {self.results_dir / 'search_summary.csv'}")

# -------------------------------------------------------------------
# Combined pipeline: sets up dirs, preprocess if needed, runs RCRA/FRS
# -------------------------------------------------------------------
def setup_search_dirs(input_path: Path) -> Dict[str, Path]:
    base = settings.RESULTS_ROOT / "searches" / input_path.stem
    dirs = {
        'base': base,
        'rcra': base / 'rcra',
        'rcra_matches': base / 'rcra' / 'matches',
        'frs': base / 'frs',
        'frs_matches': base / 'frs' / 'matches'
    }
    for d in dirs.values():
        d.mkdir(parents=True, exist_ok=True)
    return dirs

def combine_match_files(match_dir: Path, parent_dir: Path) -> None:
    """
    Combine all CSVs in `match_dir` (except search_summary.csv) into a single
    all_matches.csv in `parent_dir`.
    Also move the summary file out.
    """
    match_files = list(match_dir.glob("*.csv"))
    if not match_files:
        return
    # Exclude the summary
    csvs = [f for f in match_files if f.name != 'search_summary.csv']
    if csvs:
        combined = pd.concat([pd.read_csv(f) for f in csvs], ignore_index=True)
        combined.to_csv(parent_dir / "all_matches.csv", index=False)
    summary = match_dir / "search_summary.csv"
    if summary.exists():
        shutil.move(str(summary), str(parent_dir / "search_summary.csv"))

def preprocess_if_needed(search_terms_path: Path) -> Path:
    """
    Checks if search_terms CSV already has columns 
    [account_id, account_name, name_terms, email_terms].
    If not, calls the 'process_search_terms' routine.
    """
    df = pd.read_csv(search_terms_path)
    needed = ['account_id','account_name','name_terms','email_terms']
    has_all = all(col in df.columns for col in needed)
    if has_all:
        return search_terms_path
    
    print("Detected that search_terms CSV does not have the required columns. Preprocessing...")
    processed_dir = settings.PROCESSED_DATA_DIR / "search_terms"
    processed_dir.mkdir(parents=True, exist_ok=True)
    stamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    out_path = processed_dir / f"{search_terms_path.stem}_{stamp}_processed.csv"
    process_search_terms(str(search_terms_path), str(out_path))
    return out_path

def run_search_pipeline(search_terms_path: Path, run_rcra: bool=True, run_frs: bool=True) -> Dict[str, Path]:
    dirs = setup_search_dirs(search_terms_path)
    # Possibly preprocess
    final_search_csv = preprocess_if_needed(search_terms_path)

    outputs = {}
    if run_rcra:
        # Run RCRA
        searcher = RCRASearcher(final_search_csv, dirs['rcra_matches'])
        searcher.run()
        combine_match_files(dirs['rcra_matches'], dirs['rcra'])
        outputs['rcra'] = dirs['rcra']

    if run_frs:
        # Run FRS
        searcher = FRSSearcher(final_search_csv, dirs['frs_matches'])
        searcher.run()
        combine_match_files(dirs['frs_matches'], dirs['frs'])
        outputs['frs'] = dirs['frs']

    return outputs

# -------------------------------------------------------------------
# CLI
# -------------------------------------------------------------------
def main():
    # Load input JSON
    input_path = Path(sys.argv[1])
    inp = json.loads(input_path.read_text())
    # Example: inp = {"input_file": ..., "output_dir": ..., "run_rcra": ..., "run_frs": ...}

    # TODO: Replace below with actual logic using inp fields
    print(f"Loaded input: {inp}")
    # ... rest of your pipeline logic ...

    # If you later add out.schema.json, emit a structured result like:
    # result = {"summary": "..."}
    # print("##DATA##" + json.dumps(result))

if __name__ == "__main__":
    main()