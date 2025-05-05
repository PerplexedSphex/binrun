# Test Script (`main.py`)

This Python script demonstrates basic script execution and output handling within the `binrun` environment.

## Functionality

1.  **Prints Python Version:** Outputs the version of the Python interpreter being used.
2.  **Iterates 1-20:** Loops through numbers from 1 to 20.
3.  **Calculates and Prints:** For each number `i`, it attempts to calculate `1 / (i % 4)` and prints the result to standard output.
4.  **Handles Errors:** If `i` is a multiple of 4, `i % 4` is 0, causing a `ZeroDivisionError`. The script catches this error and prints an error message to standard error.
5.  **Pauses:** Sleeps for 0.5 seconds between each iteration.

## Purpose

This script is useful for testing:
*   Script execution workflow.
*   Capturing and displaying `stdout`.
*   Capturing and displaying `stderr` (especially for errors).
*   Handling scripts that produce output over time.
