# Quick Start: Using Ledit Issue Solver Right Now

## Fastest Way (Without Publishing)

You can use the action directly from the ledit repository:

```yaml
name: AI Issue Solver

on:
  issue_comment:
    types: [created]

permissions:
  contents: write
  issues: write
  pull-requests: write

jobs:
  solve-issue:
    if: |
      github.event.issue.pull_request == null && 
      contains(github.event.comment.body, '/ledit')
    runs-on: ubuntu-latest
    
    steps:
      - uses: actions/checkout@v4
      
      - uses: alantheprice/ledit/.github/actions/ledit-issue-solver@main
        with:
          ai-provider: 'groq'  # Groq has a free tier!
          ai-model: 'llama-3.1-70b-versatile'
          github-token: ${{ secrets.GITHUB_TOKEN }}
          ai-api-key: ${{ secrets.GROQ_API_KEY }}
```

## Step by Step

### 1. Get a Free API Key (Groq)

Go to https://console.groq.com/keys and create a free account to get an API key.

### 2. Add the API Key to Your Repository

1. Go to your repository Settings
2. Click on "Secrets and variables" → "Actions"
3. Click "New repository secret"
4. Name: `GROQ_API_KEY`
5. Value: Your Groq API key

### 3. Create the Workflow

Create `.github/workflows/ai-solver.yml` in your repository with the content above.

### 4. Test It!

1. Create an issue: "Add a function to calculate fibonacci numbers"
2. Comment: `/ledit`
3. Watch the magic!

## Other Provider Options

**OpenAI (Most Capable)**
```yaml
ai-provider: 'openai'
ai-model: 'gpt-4o-mini'  # Cheaper option
ai-api-key: ${{ secrets.OPENAI_API_KEY }}
```

**Google Gemini (Good Balance)**
```yaml
ai-provider: 'gemini'
ai-model: 'gemini-1.5-flash'
ai-api-key: ${{ secrets.GEMINI_API_KEY }}
```

**DeepInfra (Open Models)**
```yaml
ai-provider: 'deepinfra'
ai-model: 'meta-llama/Llama-3.3-70B-Instruct'
ai-api-key: ${{ secrets.DEEPINFRA_API_KEY }}
```

That's it! You're ready to use AI-powered issue solving in your repository.