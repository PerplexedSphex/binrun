## Instructions for Fixing the Architecture

### Phase 1: Fix the Message Schema

1. **Update ScriptRunCommand**
   - Remove the `json:"-"` tag from ScriptName field
   - Change Subject() method to return a constant `"command.script.run"`

2. **Update all Command subjects to be static**
   - Change `ScriptRunSubjectPattern = "command.script.*.run"` to `ScriptRunSubject = "command.script.run"`
   - Ensure all commands return static subjects, no fmt.Sprintf in Command Subject() methods

3. **Keep Event subjects dynamic** (they're fine as-is for subscription filtering)

### Phase 2: Extend BuildCommand

4. **Add all command types to BuildCommand**
   - Add case for "TerminalCommandMessage"
   - Add cases for any other command types in the system
   - Each case should build the complete command from the data map

5. **Add command registry function**
   ```go
   // Add new function to messages package
   func GetCommandTypes() []string {
       return []string{
           "ScriptCreateCommand",
           "ScriptRunCommand", 
           "TerminalCommandMessage",
           // ... all others
       }
   }
   ```

### Phase 3: Simplify HTTP Handler

6. **Delete all special-case handling in SendCommand**
   - Remove the `if path == "execute"` block - this becomes the ONLY path
   - Remove the `else if strings.HasPrefix(path, "script/")` block entirely
   - Remove the legacy raw command handling block

7. **Make SendCommand only handle typed commands**
   - Parse request body (JSON or form) into `map[string]any`
   - Extract `_messageType` field
   - Call BuildCommand
   - Validate
   - Publish using typed publisher
   - Return standardized response

8. **Standardize form parsing**
   - When parsing form data, always put single values as strings, multiple values as arrays
   - Don't try to parse env vars from newline-separated strings - expect proper JSON structure
   - If you need to support legacy form formats, convert them to the expected structure in one place

### Phase 4: Update HTTP Routes

9. **Change HTTP routing**
   - Route all commands to `/command` (POST only)
   - Remove `/command/{path}` wildcard routing
   - Remove `/command/script/{scriptName}/run` pattern

10. **Update any clients/UI**
    - All commands now POST to `/command` with JSON body
    - Body must include `_messageType` field
    - Script name goes in the body, not the URL

### Phase 5: Clean Up

11. **Remove unused subject builder functions**
    - Remove `ScriptRunSubject(scriptName string)` function - no longer needed
    - Keep event subject builders as they still use dynamic patterns

12. **Update Publisher if needed**
    - Ensure it works with all command types
    - Remove any special handling for dynamic command subjects

### Example of Final Usage

After these changes, ALL commands work like this:

```
POST /command
Content-Type: application/json

{
  "_messageType": "ScriptRunCommand",
  "script_name": "my-script",
  "args": ["--verbose", "--debug"],
  "env": {"NODE_ENV": "production"}
}
```

No more special URLs, no more extracting data from paths, no more multiple code paths.