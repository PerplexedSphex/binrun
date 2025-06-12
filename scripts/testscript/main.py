import sys
import json
import time
from pathlib import Path


def main():
    # Load input JSON (not used, but required by protocol)
    input_path = Path(sys.argv[1])
    _ = json.loads(input_path.read_text())

    # 1. print python version
    print(f"Python version: {sys.version}")

    # 2. loop 1..20 sleeping 0.5s each
    for i in range(1, 21):
        try:
            mod = i % 4
            result = 1 / mod  # will raise when mod == 0
            print(f"{i}: 1/(i%4) = {result}")
        except ZeroDivisionError as exc:
            print(f"{i}: encountered error -> {exc}", file=sys.stderr)
        time.sleep(0.5)


if __name__ == "__main__":
    main()
