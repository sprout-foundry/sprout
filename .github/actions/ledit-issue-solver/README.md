# Ledit Issue Solver GitHub Action

Automatically solve GitHub issues using AI-powered code generation with [ledit](https://github.com/your-org/ledit).

## Overview

This action allows you to use ledit to automatically implement features and fix bugs described in GitHub issues. Simply comment `/ledit` on an issue, and the action will:

1. Analyze the issue description and comments
2. Create a new branch for the implementation
3. Generate the necessary code changes
4. Create a pull request with the implementation
5. Report progress back to the issue

## Usage

### Basic Setup

1. Add this workflow to your repository (`.github/workflows/ledit-solver.yml`):

```yaml
name: Ledit Issue Solver

on:
  issue_comment:
    types: [created]

jobs:
  solve-issue:
    if: contains(github.event.comment.body, '/ledit')
    runs-on: ubuntu-latest
    
    steps:
      - uses: actions/checkout@v4
      
      - uses: ./.github/actions/ledit-issue-solver@v1
        with:
          provider: 'openai'  # or 'openrouter', 'groq', etc.
          model: 'gpt-4'      # or any supported model
          github-token: ${{ secrets.GITHUB_TOKEN }}
          api-key: ${{ secrets.OPENAI_API_KEY }}
```

2. Add your API key as a repository secret

3. Comment `/ledit` on any issue to trigger the solver

### Supported Providers

- **OpenAI**: `gpt-4`, `gpt-4-turbo`, `gpt-3.5-turbo`
- **OpenRouter**: Any model available on OpenRouter
- **Groq**: `llama3-70b-8192`, `mixtral-8x7b-32768`
- **Gemini**: `gemini-pro`, `gemini-pro-vision`
- **DeepInfra**: Various open-source models
- **Ollama**: Local models (requires self-hosted runner)

### Advanced Configuration

```yaml
- uses: ./.github/actions/ledit-issue-solver@v1
  with:
    provider: 'openrouter'
    model: 'anthropic/claude-3-opus'
    github-token: ${{ secrets.GITHUB_TOKEN }}
    api-key: ${{ secrets.OPENROUTER_API_KEY }}
    
    # Optional parameters
    timeout-minutes: 30              # Max time for ledit to run (default: 20)
    branch-prefix: 'fix'            # Custom branch prefix (default: 'issue')
    ledit-version: 'latest'         # Specific ledit version (default: latest)
    max-cost: '5.00'                # Maximum cost limit in USD
    additional-context: |           # Extra context for the AI
      Please follow our coding standards in CONTRIBUTING.md
      Use TypeScript for all new code
```

## Features

### Iterative Development

The action supports iterative development. After the initial implementation:

1. Review the generated PR
2. Comment `/ledit <additional instructions>` on the issue
3. The action will update the existing PR with your requested changes

### Image Support

The action automatically downloads and analyzes images attached to issues. This is useful for:
- UI/UX implementations based on mockups
- Bug reports with screenshots
- Architecture diagrams

### MCP Integration

If your repository has MCP (Model Context Protocol) servers configured, the action will automatically use them, providing the AI with:
- Access to external APIs
- Database queries
- Custom tools specific to your project

### Progress Tracking

The action posts status updates to the issue:
- When starting analysis
- After creating the branch
- When the PR is ready
- If any errors occur

## Examples

### Simple Bug Fix

```markdown
**Issue Title**: Button doesn't change color on hover

**Description**: The submit button in the login form should turn blue on hover but currently stays gray.

**Comment**: /ledit
```

### Feature Implementation

```markdown
**Issue Title**: Add dark mode support

**Description**: 
- Add a toggle in the settings menu
- Store preference in localStorage  
- Apply theme to all components
- Include smooth transitions

**Comment**: /ledit use Tailwind CSS for styling
```

### With Mockup

```markdown
**Issue Title**: Implement new dashboard layout

**Description**: Please implement the dashboard according to the attached mockup.

**Attachments**: dashboard-mockup.png

**Comment**: /ledit match the colors and spacing exactly as shown
```

## Security Considerations

- API keys are never exposed in logs or PR descriptions
- The action only has access to the repository it's running in
- Generated code is always reviewed via PR before merging
- Branch protection rules are respected

## Troubleshooting

### Common Issues

1. **"No tools available" error**
   - Ensure MCP servers are properly configured if using them
   - Check that the GITHUB_TOKEN has necessary permissions

2. **"Context too large" error**
   - The repository might be too large
   - Add a `.leditignore` file to exclude unnecessary files

3. **"API rate limit" error**
   - Consider using a model with higher rate limits
   - Implement caching for repeated runs

### Debug Mode

Enable debug logging by setting the `ACTIONS_STEP_DEBUG` secret to `true`.

## Contributing

Contributions are welcome! Please see the main [ledit repository](https://github.com/your-org/ledit) for development guidelines.

## License

This action is part of the ledit project and follows the same license.