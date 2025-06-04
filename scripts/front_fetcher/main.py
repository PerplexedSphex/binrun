import os
import sys
import json
import requests

def main():
    # Read input
    with open(sys.argv[1], "r") as f:
        input_data = json.load(f)
    limit = input_data.get("limit", 5)

    # Get API token from env
    token = os.environ["FRONT_API_TOKEN"]
    headers = {
        "Authorization": f"Bearer {token}",
        "Accept": "application/json"
    }

    # Fetch conversations
    url = f"https://api2.frontapp.com/conversations?limit={limit}"
    resp = requests.get(url, headers=headers)
    resp.raise_for_status()
    conversations = resp.json().get("_results", [])

    # Prepare output
    emails = [{"id": c["id"], "subject": c.get("subject", "")} for c in conversations]
    output = {"emails": emails}

    # Emit as NATS event (via stdout, runner will pick up)
    print("##DATA##" + json.dumps(output))

if __name__ == "__main__":
    main() 