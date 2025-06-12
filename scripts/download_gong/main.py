import requests
import json
import sys
import time
import os
import base64
from datetime import datetime
import pytz
from pathlib import Path

SCRIPT_DIR = Path(__file__).parent.resolve()

# Function to fetch paginated data from Gong API
def retrieve_pages(endpoint, filter_params, content_selector=None, output_file=None, records_key=None):
    cursor = None
    while True:
        # Prepare request body
        body = {"filter": filter_params}
        if content_selector:
            body["contentSelector"] = content_selector
        if cursor:
            body["cursor"] = cursor

        # Make API request
        try:
            response = requests.post(f"{base_url}{endpoint}", headers=headers, json=body)
            response.raise_for_status()  # Raise exception for HTTP errors
        except requests.RequestException as e:
            print(f"Error fetching data from {endpoint}: {e}")
            break

        # Parse response
        data = response.json()
        records = data.get(records_key, [])
        if not records:
            break

        # Write records to file
        with open(output_file, "a") as f:
            for record in records:
                f.write(json.dumps(record) + "\n")

        # Get next cursor
        cursor = data.get("records", {}).get("cursor")
        if not cursor:
            break

        # Respect rate limit: 3 calls per second
        time.sleep(0.34)

# Define content selector for extensive calls
content_selector = {
    "context": "Extended",
    "contextTiming": ["Now", "TimeOfCall"],
    "exposedFields": {
        "parties": True,
        "content": {
            "structure": True,
            "topics": True,
            "trackers": True,
            "trackerOccurrences": True,
            "pointsOfInterest": True,
            "brief": True,
            "outline": True,
            "highlights": True,
            "callOutcome": True,
            "keyPoints": True
        },
        "interaction": {
            "speakers": True,
            "video": True,
            "personInteractionStats": True,
            "questions": True
        },
        "collaboration": {
            "publicComments": True
        },
        "media": True
    }
}

# Main execution
def main():
    global base_url, headers
    # Load input JSON
    input_path = Path(sys.argv[1])
    inp = json.loads(input_path.read_text())

    # Retrieve Gong API credentials
    access_key = os.environ.get("GONG_ACCESS_KEY")
    access_key_secret = os.environ.get("GONG_ACCESS_KEY_SECRET")
    base_url = os.environ.get("GONG_BASE_URL")
    if not all([access_key, access_key_secret, base_url]):
        raise ValueError("Missing Gong API credentials in environment variables")
    auth_string = f"{access_key}:{access_key_secret}"
    auth_token = base64.b64encode(auth_string.encode()).decode()
    headers = {
        "Authorization": f"Basic {auth_token}",
        "Content-Type": "application/json"
    }

    # Determine date range
    from_iso = inp.get("fromDateTime")
    to_iso = inp.get("toDateTime")
    if from_iso:
        from_dt = from_iso
    else:
        from_dt = datetime(2010, 1, 1, tzinfo=pytz.UTC).isoformat()
    if to_iso:
        to_dt = to_iso
    else:
        to_dt = datetime.now(pytz.UTC).isoformat()
    filter_params = {"fromDateTime": from_dt, "toDateTime": to_dt}

    # Determine output directory: input override, env, or default path
    default_output = SCRIPT_DIR.parent.parent / "store" / "script_output_files" / "gong_download"
    output_dir = Path(inp.get("output_dir") or os.getenv("OUTPUT_DIR") or default_output)
    output_dir.mkdir(parents=True, exist_ok=True)
    extensive_file = output_dir / inp.get("extensive_file", "extensive_calls.jsonl")
    transcript_file = output_dir / inp.get("transcript_file", "transcripts.jsonl")

    # Clear existing files if they exist
    if extensive_file.exists():
        extensive_file.unlink()
    if transcript_file.exists():
        transcript_file.unlink()

    # Fetch extensive call data
    print("Fetching extensive call data...")
    retrieve_pages(
        endpoint="/v2/calls/extensive",
        filter_params=filter_params,
        content_selector=inp.get("contentSelector", content_selector),
        output_file=str(extensive_file),
        records_key="calls"
    )
    print(f"Extensive call data saved to {extensive_file}")

    # Fetch transcripts
    print("Fetching transcripts...")
    retrieve_pages(
        endpoint="/v2/calls/transcript",
        filter_params=filter_params,
        output_file=str(transcript_file),
        records_key="callTranscripts"
    )
    print(f"Transcripts saved to {transcript_file}")

    # Print structured result
    result = {"status": "success", "extensive_file": str(extensive_file), "transcript_file": str(transcript_file)}
    print("##DATA##" + json.dumps(result))

if __name__ == "__main__":
    main()