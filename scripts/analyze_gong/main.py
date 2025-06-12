#!/usr/bin/env python3
"""
call_transcript_summarizer.py
Filters calls by Salesforce account, turns each transcript into structured
JSON (validated by Pydantic), then feeds all summaries into a second Gemini
call to generate a single executive brief.
"""

import sys
import json
import os
from pathlib import Path
from typing import Any, Dict, List, Optional
import time  # for timing individual summaries

from google import genai                    # google-genai ≥ 1.0.0
from google.genai import types              # for GenerateContentConfig
from tqdm import tqdm

from prompts.call_analysis_schema import CallAnalysis              # ← external Pydantic file

# ───────────────────────────────────────── Paths ─────────────────────────────────────────
SCRIPT_DIR = Path(__file__).parent.resolve()
# Default input paths for calls and transcripts
CALLS_JSONL_PATH       = SCRIPT_DIR.parent.parent / "store" / "script_output_files" / "gong_download" / "extensive_calls.jsonl"
TRANSCRIPTS_JSONL_PATH = SCRIPT_DIR.parent.parent / "store" / "script_output_files" / "gong_download" / "transcripts.jsonl"
# Prompt paths
STRUCTURED_PROMPT_PATH = Path("prompts/call_analysis_instructions.md")
FINAL_PROMPT_PATH      = Path("prompts/synthesis.md")
# Default output directory for analysis results
DEFAULT_OUTPUT_DIR     = SCRIPT_DIR.parent.parent / "store" / "script_output_files" / "gong_analysis"
MODEL_FAST             = "gemini-2.5-flash-preview-05-20"
MODEL_FINAL            = "gemini-2.5-pro-preview-06-05"
# ─────────────────────────────────────────────────────────────────────────────────────────

# ---------- 2. Helpers ------------------------------------------------

def load_jsonl(fp: Path) -> List[Dict]:
    with fp.open(encoding="utf-8") as f:
        return [json.loads(line) for line in f if line.strip()]

def save_jsonl(data: List[Dict[str, Any]], out_path: Path) -> None:
    out_path.parent.mkdir(parents=True, exist_ok=True)
    with out_path.open("w", encoding="utf-8") as f:
        for item in data:
            f.write(json.dumps(item, ensure_ascii=False) + "\n")

def get_company_name(call: Dict) -> Optional[str]:
    for ctx in call.get("context", []):
        if ctx.get("system") == "Salesforce":
            for obj in ctx.get("objects", []):
                if obj.get("objectType") == "Account":
                    for field in obj.get("fields", []):
                        if field.get("name") == "Name":
                            return field.get("value")
    return None

def filter_account_calls(calls: List[Dict], account_substr: str) -> List[Dict]:
    account_calls = []
    for c in tqdm(calls, desc=f'Filtering for "{account_substr}"'):
        name = get_company_name(c)
        if name and account_substr.lower() in name.lower():
            account_calls.append(c)
    return account_calls

def create_participant_map(call: Dict) -> Dict[str, Dict]:
    mp = {}
    for p in call.get("parties", []):
        sid = p.get("speakerId")
        if sid:
            mp[sid] = {
                "name":        p.get("name", "Unknown"),
                "title":       p.get("title", "No Title"),
                "affiliation": p.get("affiliation", "Unknown"),
            }
    return mp

def flatten_transcript(transcript: Dict, call: Dict) -> str:
    meta   = call.get("metaData", {})
    people = create_participant_map(call)

    lines = [
        f"Call ID: {meta.get('id')}",
        f"Title: {meta.get('title')}",
        f"Date: {meta.get('started')}",
        f"Duration: {meta.get('duration')} seconds",
        "",
        "Participants:",
        *[
            f"- {info['name']} ({info['title']}) – {info['affiliation']}"
            for info in people.values()
        ],
        "",
        "Transcript:"
    ]

    for topic in transcript.get("transcript", []):
        tname = topic.get("topic", "Unnamed Topic")
        speaker = people.get(topic.get("speakerId"), {"name": "Unknown"})
        lines.append(f"\nTopic: {tname}")
        for sentence in topic.get("sentences", []):
            txt = sentence.get("text", "").strip()
            if txt:
                lines.append(f"{speaker['name']}: {txt}")

    return "\n".join(lines)

# ───────── Gemini helpers ───────────────────────────────────────────────────────────────
def init_client() -> genai.Client:
    api_key = os.getenv("GEMINI_API_KEY")
    if not api_key:
        raise EnvironmentError("export GEMINI_API_KEY first")
    return genai.Client(api_key=api_key)

def summarize_call_structured(client: genai.Client, prompt: str, transcript: str) -> CallAnalysis:
    cfg = types.GenerateContentConfig(
        response_mime_type="application/json",
        response_schema=CallAnalysis,        # hand in the Pydantic class ✔
        temperature=0.3,
    )
    resp = client.models.generate_content(
        model=MODEL_FAST,
        contents=f"{prompt}\n\n{transcript}",
        config=cfg,
    )
    return resp.parsed                     # → CallSummary instance

def create_final_brief(client: genai.Client, prompt: str, rollup_text: str) -> str:
    resp = client.models.generate_content(
        model=MODEL_FINAL,
        contents=f"{prompt}\n\n{rollup_text}",
        config={"temperature": 0.4},
    )
    return resp.text.strip()

# ───────── Main orchestration ───────────────────────────────────────────────────────────
def main() -> None:
    # Load input JSON (ScriptRunner payload)
    input_path = Path(sys.argv[1])
    inp = json.loads(input_path.read_text())
    account = inp.get("account")
    if not account:
        raise ValueError("Missing 'account' in input")

    # 🚀 Starting log
    print(f"🚀 Starting analysis for account: {account}", flush=True)

    # Resolve input file paths
    calls_path = Path(inp.get("calls_jsonl_path") or CALLS_JSONL_PATH)
    transcripts_path = Path(inp.get("transcripts_jsonl_path") or TRANSCRIPTS_JSONL_PATH)
    # Determine output directory early for summaries and final brief
    output_dir = Path(inp.get("output_dir") or os.getenv("OUTPUT_DIR") or DEFAULT_OUTPUT_DIR)
    output_dir.mkdir(parents=True, exist_ok=True)
    # Summaries JSONL path
    summaries_file = output_dir / inp.get("summaries_file", "call_summaries.jsonl")
    # Open summaries file
    sf = summaries_file.open("w", encoding="utf-8")

    calls       = load_jsonl(calls_path)
    print(f"📂 Loaded {len(calls)} calls from {calls_path}", flush=True)
    transcripts = load_jsonl(transcripts_path)
    print(f"📂 Loaded {len(transcripts)} transcripts from {transcripts_path}", flush=True)

    # 1. Filter & match
    print(f"🔍 Filtering calls for account: {account}", flush=True)
    account_calls = filter_account_calls(calls, account)
    # Optional duration filter
    min_dur = inp.get("min_duration")
    max_dur = inp.get("max_duration")
    if min_dur is not None or max_dur is not None:
        conds = []
        if min_dur is not None:
            conds.append(f">= {min_dur}s")
        if max_dur is not None:
            conds.append(f"<= {max_dur}s")
        print(f"⏱ Applying duration filter {' and '.join(conds)}", flush=True)
        filtered_calls = []
        for c in account_calls:
            dur = c.get("metaData", {}).get("duration", 0)
            if min_dur is not None and dur < min_dur:
                continue
            if max_dur is not None and dur > max_dur:
                continue
            filtered_calls.append(c)
        print(f"✅ {len(filtered_calls)}/{len(account_calls)} calls after duration filter", flush=True)
        account_calls = filtered_calls
    ids           = {c["metaData"]["id"] for c in account_calls}
    matched_tx    = [t for t in transcripts if t.get("callId") in ids]
    if not matched_tx:
        print("⚠️ No transcripts located.", flush=True)
        # Emit empty structured result
        result = {"status": "empty", "message": "No transcripts located"}
        print("##DATA##" + json.dumps(result), flush=True)
        return
    else:
        print(f"✅ Found {len(matched_tx)} matching transcripts for account: {account}", flush=True)

    # 2. Gemini client initialization
    print("🔑 Initializing Gemini client...", flush=True)
    client = init_client()
    print("🤖 Gemini client ready", flush=True)
    per_call_prompt = STRUCTURED_PROMPT_PATH.read_text(encoding="utf-8")

    # 3. Summarization with streaming to JSONL and progress logs
    print("🤖 Summarizing calls...", flush=True)
    total = len(matched_tx)
    summaries = []
    for idx, tx in enumerate(matched_tx, start=1):
        start_time = time.perf_counter()
        call = next(c for c in account_calls if c["metaData"]["id"] == tx["callId"])
        flat_txt = flatten_transcript(tx, call)
        summary = summarize_call_structured(client, per_call_prompt, flat_txt)
        # write summary JSONL line
        sf.write(summary.model_dump_json() + "\n")
        elapsed = time.perf_counter() - start_time
        print(f"✅ Completed summary {idx}/{total} in {elapsed:.1f}s", flush=True)
        summaries.append(summary)
    sf.close()
    print(f"🧮 Completed summarizing all {len(summaries)} calls, summaries saved to {summaries_file}", flush=True)

    # 4. Second-pass aggregation
    print("🔗 Aggregating summaries...", flush=True)
    rollup_text = "\n\n".join(s.model_dump_json(indent=None) for s in summaries)
    final_prompt = FINAL_PROMPT_PATH.read_text(encoding="utf-8")
    executive_brief = create_final_brief(client, final_prompt, rollup_text)

    # 4. Persist final brief
    out_file = output_dir / f"{account.replace(' ', '_')}_executive_brief.txt"
    out_file.write_text(executive_brief, encoding="utf-8")
    print(f"✅ Executive brief saved to {out_file}", flush=True)
    # Emit structured result
    result = {"status": "success", "summaries_file": str(summaries_file), "executive_brief_file": str(out_file)}
    print("##DATA##" + json.dumps(result), flush=True)

if __name__ == "__main__":
    main()
