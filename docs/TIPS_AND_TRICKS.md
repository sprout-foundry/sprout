# Advanced ledit Techniques

## Effective Prompting

1. **Be Specific**:

   ```bash
   # Instead of:
   ledit code "Make it faster"
   # Use:
   ledit code "Optimize database queries to use indexes" --filename db.py
   ```

2. **Chain Changes**:

   ```bash
   ledit code "Create interface" --filename service.go && \
   ledit code "Implement concrete class" --filename service_impl.go
   ```

3. **Leverage Workspace Context**:

   # Use #WS to include relevant files from your workspace as context.

   ```bash
   ledit code "Refactor the user authentication logic #WS"
   ```

4. **Ground with Web Search**:
   # Use #SG "query" to augment your prompt with fresh information from the web.
   ```bash
   ledit code "Implement OAuth2 authentication using PKCE flow #SG \"OAuth2 PKCE flow best practices\""
   ```

## Workspace Optimization

1. **Custom Ignore Patterns**:
   ```bash
   ledit ignore "node_modules/"
   ledit ignore "*.tmp"
   ```

## Debugging Techniques

1. **View Change History and Revert Changes**:

   ```bash
   ledit log
   ```

2. **Fix Code**:

   ```bash
   go build 2> error.txt
   ledit code "Fix this error: \n\n$(cat error.txt)\n\n Use the workspace context: #WS"
   ```

   OR, use the ledit fix shortcut to do the same:

   ```bash
   ledit fix "go build"
   ```

3. **Get Explanations**:
   ```bash
   ledit question "What is the structure of the create user request sent to the api?"
   ```

## Ideas

1. **Batch Processing**:

   ```bash
   for file in *.js; do
     ledit code "Add error handling" --filename "$file"
   done
   ```

2. **See Examples folder for additional ides**
