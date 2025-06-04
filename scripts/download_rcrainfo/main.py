# etl.py

import sys
import json
import logging
import os
from pathlib import Path
import re
import shutil
import zipfile
from datetime import datetime, timedelta

import requests
from tqdm import tqdm

import duckdb

# ---------------------------------------------------------------------
# Config / Settings
# (Replace these with whatever config system you actually use)
# ---------------------------------------------------------------------
DATA_ROOT = Path.cwd() / "data"
RAW_DATA_DIR = DATA_ROOT / "raw"
PROCESSED_DATA_DIR = DATA_ROOT / "processed"

# For demonstration, only listing a few:
DATASETS = {
    "rcra": {
        "subfolder": "rcra",
        "url_pattern": "https://s3.amazonaws.com/rcrainfo-ftp/Production/CSV-{date}/Handler/HD.zip"
    },
    "ce": {
        "subfolder": "ce",
        "url_pattern": "https://s3.amazonaws.com/rcrainfo-ftp/Production/CSV-{date}/Compliance,%20Monitoring%20and%20Enforcement/CE.zip"
    },
    "ca": {
        "subfolder": "ca",
        "url_pattern": "https://s3.amazonaws.com/rcrainfo-ftp/Production/CSV-{date}/Corrective%20Action/CA.zip"
    },
    "br": {
        "subfolder": "br",
        "url_pattern": "https://s3.amazonaws.com/rcrainfo-ftp/Production/CSV-{date}/Biennial%20Report/BR.zip"
    },
    "emanifest": {
        "subfolder": "emanifest",
        "url_pattern": "https://s3.amazonaws.com/rcrainfo-ftp/Production/CSV-{date}/eManifest/EM.zip"
    },
    # Add more datasets or special rules as needed
}

DB_PATH = PROCESSED_DATA_DIR / "rcra" / "rcrainfo.duckdb"

# ---------------------------------------------------------------------
# Logging
# ---------------------------------------------------------------------
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)


# ---------------------------------------------------------------------
# Utility functions
# ---------------------------------------------------------------------
def get_latest_monday_date() -> str:
    """
    Get the most recent Monday's date in the format: YYYY-MM-DDTHH-MM-SS-0400
    Example: '2025-01-27T03-00-00-0400'
    """
    today = datetime.now()
    monday = today - timedelta(days=today.weekday())  # go back to Monday
    return monday.strftime("%Y-%m-%dT03-00-00-0400")


def download_file(url: str, output_path: Path, chunk_size: int = 8192) -> Path:
    """
    Download a file from a URL with a progress bar.
    """
    output_path.parent.mkdir(parents=True, exist_ok=True)
    logger.info(f"Downloading {url} to {output_path}...")

    resp = requests.get(url, stream=True)
    resp.raise_for_status()

    total_size = int(resp.headers.get("content-length", 0))
    with tqdm(total=total_size, unit="iB", unit_scale=True) as pbar, open(output_path, "wb") as f:
        for chunk in resp.iter_content(chunk_size=chunk_size):
            if chunk:
                f.write(chunk)
                pbar.update(len(chunk))
    return output_path


def unzip_recursive(zip_path: Path, extract_dir: Path) -> None:
    """
    Recursively unzip files, extracting nested zips as needed.
    """
    logger.info(f"Unzipping {zip_path} into {extract_dir}...")
    with zipfile.ZipFile(zip_path, 'r') as zip_ref:
        zip_ref.extractall(extract_dir)

    # Check for nested zips
    for root, _, files in os.walk(extract_dir):
        for name in files:
            fpath = Path(root) / name
            if fpath.suffix.lower() == '.zip':
                # create a subfolder named after the zip
                nested_dir = fpath.parent / fpath.stem
                nested_dir.mkdir(exist_ok=True)
                unzip_recursive(fpath, nested_dir)
                fpath.unlink()  # remove the nested zip after extraction


def download_and_extract(
    base_dir: Path,
    subfolder_name: str,
    url: str,
    date_str: str,
    cleanup_zip: bool = True
) -> Path:
    """
    Download a ZIP from `url`, extract it to a date-based folder inside `base_dir`,
    optionally remove the original zip. Return the extraction directory path.
    """
    extract_dir = base_dir / date_str / subfolder_name

    # Clean up if directory already exists
    if extract_dir.exists():
        logger.info(f"Clearing existing directory: {extract_dir}")
        shutil.rmtree(extract_dir)

    extract_dir.mkdir(parents=True, exist_ok=True)

    # Download
    zip_path = base_dir / f"{subfolder_name}.zip"
    download_file(url, zip_path)

    # Extract
    unzip_recursive(zip_path, extract_dir)

    # Cleanup
    if cleanup_zip:
        zip_path.unlink()

    return extract_dir


def ensure_db_dir(db_path: Path) -> None:
    """
    Ensure the directory for a DuckDB file exists.
    """
    db_path.parent.mkdir(parents=True, exist_ok=True)


def to_snake_case(name: str) -> str:
    """Convert a column name to snake_case format."""
    # Convert to lowercase first
    s = name.lower()
    # Replace spaces and special characters with underscores
    s = re.sub(r'[^a-z0-9]+', '_', s)
    # Remove leading/trailing underscores
    s = s.strip('_')
    # Collapse multiple underscores
    s = re.sub(r'_+', '_', s)
    return s


def process_data_folder(folder: Path, table_name: str) -> None:
    """
    Load all CSVs under `folder` into a single DuckDB table named `table_name`.
    Converts all column names to snake_case format.
    """
    con = duckdb.connect(str(DB_PATH))
    try:
        # Drop old table if it exists
        con.execute(f"DROP TABLE IF EXISTS {table_name}")
        
        # Gather all CSVs
        csv_files = list(folder.glob("*.csv"))
        if not csv_files:
            logger.info(f"No CSV files found in {folder}. Skipping.")
            return

        logger.info(f"Stacking {len(csv_files)} CSV(s) for table {table_name}...")

        # Read first CSV to get column names and convert them to snake_case
        sample_df = con.execute(
            f"SELECT * FROM read_csv_auto('{csv_files[0]}', header=True, ALL_VARCHAR=TRUE) LIMIT 0"
        ).fetchdf()
        original_columns = sample_df.columns.tolist()
        snake_case_columns = [to_snake_case(col) for col in original_columns]
        
        # Build column renaming part of the query
        column_renames = [
            f'"{orig}" as {snake}' for orig, snake in zip(original_columns, snake_case_columns)
        ]
        select_clause = ", ".join(column_renames)

        # Build one UNION ALL query from multiple CSVs with renamed columns
        union_parts = [
            f"SELECT {select_clause} FROM read_csv_auto('{csv_file}', header=True, ALL_VARCHAR=TRUE)"
            for csv_file in csv_files
        ]
        union_query = " UNION ALL ".join(union_parts)

        # Create the table
        con.execute(f"CREATE TABLE {table_name} AS {union_query};")

        # Count rows
        count = con.execute(f"SELECT COUNT(*) FROM {table_name}").fetchone()[0]
        logger.info(f"Created table {table_name} with {count} rows from {folder}.")
        logger.info(f"Columns renamed to snake_case: {', '.join(snake_case_columns)}")

    finally:
        con.close()


# ---------------------------------------------------------------------
# Main ETL function
# ---------------------------------------------------------------------
def run_etl(dataset_key: str, skip_download: bool = False) -> None:
    """
    Universal ETL runner for any dataset defined in `DATASETS`.
    1) Optionally download + extract ZIP
    2) Then process CSVs into DuckDB
    """
    if dataset_key not in DATASETS:
        raise ValueError(f"Unknown dataset '{dataset_key}'. Must be one of {list(DATASETS.keys())}")

    logger.info(f"=== Starting ETL for {dataset_key} ===")
    ensure_db_dir(DB_PATH)

    # Choose or compute date
    date_str = get_latest_monday_date()
    base_dir = RAW_DATA_DIR / dataset_key
    subfolder_name = DATASETS[dataset_key]["subfolder"]
    url_pattern = DATASETS[dataset_key]["url_pattern"]
    url = url_pattern.format(date=date_str)

    # Download + Extract
    if not skip_download:
        download_and_extract(base_dir, subfolder_name, url, date_str)

    # Ingest into DuckDB
    # Typically, the extracted CSVs would be in: base_dir / date_str / subfolder_name
    dataset_dir = base_dir / date_str / subfolder_name

    if not dataset_dir.exists():
        raise FileNotFoundError(f"Extracted folder does not exist: {dataset_dir}")

    # For each immediate subdirectory that has CSVs, load them as a separate table
    # Or if everything is just in `dataset_dir` itself, do it once.
    # Adjust logic as needed. Example: for each subdir -> table_name = subdir.name
    for item in sorted(dataset_dir.iterdir()):
        if item.is_dir():
            # Example: table name = the subdir name
            process_data_folder(item, table_name=item.name)
        else:
            # If CSVs are directly in dataset_dir, you can do something like:
            if item.suffix.lower() == '.csv':
                # table name can be the dataset key or something
                process_data_folder(dataset_dir, table_name=dataset_key)
                break

    logger.info(f"=== Completed ETL for {dataset_key} ===")


# ---------------------------------------------------------------------
# Optional CLI
# ---------------------------------------------------------------------
def main():
    # Load input JSON
    input_path = Path(sys.argv[1])
    inp = json.loads(input_path.read_text())
    # inp = {"dataset": ..., "skip_download": ...}

    # TODO: Replace below with actual logic using inp fields
    print(f"Loaded input: {inp}")
    # ... rest of your ETL logic ...

    # If you later add out.schema.json, emit a structured result like:
    # result = {"summary": "..."}
    # print("##DATA##" + json.dumps(result))

if __name__ == "__main__":
    main()