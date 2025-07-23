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

## Workspace Optimization
1. **Custom Ignore Patterns**:
   ```bash
   ledit ignore "node_modules/"
   ledit ignore "*.tmp"
   ```

## Debugging Techniques
1. **View Change History**:
   ```bash
   ledit log
   ```

2. **Regenerate Code**:
   ```bash
   ledit code "Try different approach" --filename problem.py
   ```

3. **Get Explanations**:
   ```bash
   ledit question "Why was this implemented this way?"
   ```

## Performance Tips
1. **Local Models**:
   ```bash
   ledit config set local_model ollama:llama3
   ```

2. **Batch Processing**:
   ```bash
   for file in *.js; do
     ledit code "Add error handling" --filename "$file"
   done
   ```