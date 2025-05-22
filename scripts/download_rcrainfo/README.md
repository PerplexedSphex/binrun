# download_rcrainfo

Download and process EPA RCRAInfo datasets (Handler, Compliance, Corrective Action, Biennial Report, eManifest) into DuckDB, with all data stored locally in the script directory.

## Usage

### 1. From the Web Terminal (Browser UI)

In the web terminal, run:

```sh
script run download_rcrainfo rcra
```

- Replace `rcra` with any supported dataset.
- Add `--skip-download` if you want to skip downloading.

**Example:**
```sh
script run download_rcrainfo rcra --skip-download
```

The script will run in an isolated environment, and output will stream directly to your terminal.

### 2. From the Web Terminal (Direct Shell)

First, ensure dependencies are installed (the ScriptRunner does this automatically, but you can do it manually for local runs):

```sh
cd scripts/download_rcrainfo
uv venv
uv pip install .
```

Then run the script:

```sh
uv run main.py <dataset> [--skip-download]
```

- `<dataset>`: One of `rcra`, `ce`, `ca`, `br`, `emanifest`
- `--skip-download`: (optional) Only process already-downloaded data

**Example:**

```sh
uv run main.py rcra
```

### 3. From NATS (ScriptRunner)

Publish a NATS command to run the script:

```sh
nats pub command.script.download_rcrainfo.run '{ "args": ["rcra"] }'
```

- Replace `"rcra"` with any supported dataset.
- Add `"--skip-download"` to the args array if you want to skip downloading.

**Example:**

```sh
nats pub command.script.download_rcrainfo.run '{ "args": ["rcra", "--skip-download"] }'
```

The ScriptRunner will:
- Set up a Python virtual environment (if not already present)
- Install dependencies from `pyproject.toml`
- Run the script and stream logs/output via NATS events

## Output

- All data and the DuckDB database are stored in `scripts/download_rcrainfo/data/`
- Logs and progress are printed to stdout (and streamed via NATS if run that way)

## Requirements

- Python 3.11+
- [uv](https://github.com/astral-sh/uv) (for dependency management)
- NATS CLI (for NATS-based runs)

## Environment

- No API keys required; all data is public.
- If you add any config/secrets, use a `.env` file in the project root and load with `python-dotenv`.
