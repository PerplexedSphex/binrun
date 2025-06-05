import * as fs from "node:fs";

// Load input JSON (not used, but required by protocol)
console.log("testint testing 123");
const file = process.argv[2] ?? "input.json";
const input = JSON.parse(fs.readFileSync(file, "utf8"));

console.log("Hello via Bun!");