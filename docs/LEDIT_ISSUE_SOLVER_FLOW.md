# Ledit Issue Solver - Visual Flow Diagram

## Complete Workflow Flow

```mermaid
flowchart TD
    Start([GitHub Issue Comment /ledit]) --> DetectTrigger{Detect Trigger Type}
    
    DetectTrigger -->|Issue Comment| ExtractCommand[Extract /ledit Command]
    DetectTrigger -->|Workflow Dispatch| GetIssueNumber[Get Issue Number]
    DetectTrigger -->|Issue Event| GetIssueNumber
    
    ExtractCommand --> GetIssueNumber
    GetIssueNumber --> FetchIssue[Fetch Issue Data]
    
    FetchIssue --> DownloadImages[Download Embedded Images]
    FetchIssue --> FetchComments[Fetch All Comments]
    
    DownloadImages --> CreateContext[Create Context File]
    FetchComments --> CreateContext
    
    CreateContext --> SetupBranch{Branch Exists?}
    
    SetupBranch -->|Yes| CheckoutBranch[Checkout Existing Branch]
    SetupBranch -->|No| CreateBranch[Create New Branch]
    
    CheckoutBranch --> MergeUpdates[Merge Latest from Main]
    CreateBranch --> InitWorkspace[Initialize Ledit Workspace]
    MergeUpdates --> InitWorkspace
    
    InitWorkspace --> BuildPrompt[Build Agent Prompt]
    BuildPrompt --> RunLedit[Run Ledit Agent]
    
    RunLedit -->|Timeout| ReportTimeout[Report Timeout to Issue]
    RunLedit -->|Error| ReportError[Report Error to Issue]
    RunLedit -->|Success| CheckChanges{Changes Made?}
    
    CheckChanges -->|No| ReportNoChanges[Report No Changes Needed]
    CheckChanges -->|Yes| CreateCommit[Create Git Commit]
    
    CreateCommit --> PushBranch[Push to Remote]
    PushBranch --> CheckPR{PR Exists?}
    
    CheckPR -->|Yes| UpdatePR[Update Existing PR]
    CheckPR -->|No| CreatePR[Create New PR]
    
    UpdatePR --> PostComment[Post PR Link to Issue]
    CreatePR --> PostComment
    
    PostComment --> End([Complete])
    ReportTimeout --> End
    ReportError --> End
    ReportNoChanges --> End
```

## Detailed Component Interactions

### 1. Issue Data Collection Flow

```mermaid
flowchart LR
    Issue[GitHub Issue] --> API[GitHub API]
    API --> IssueJSON[issue.json]
    API --> CommentsJSON[comments.json]
    
    IssueJSON --> Parser[Content Parser]
    CommentsJSON --> Parser
    
    Parser --> ImageExtractor[Image Extractor]
    Parser --> ContextBuilder[Context Builder]
    
    ImageExtractor --> ImagesDir[/images Directory]
    ContextBuilder --> ContextMD[context.md]
```

### 2. Ledit Configuration Flow

```mermaid
flowchart TD
    Provider[AI Provider Input] --> ConfigScript[configure-ledit.sh]
    APIKey[API Key Input] --> ConfigScript
    Model[Model Input] --> ConfigScript
    
    ConfigScript --> APIKeysJSON[~/.ledit/api_keys.json]
    ConfigScript --> ConfigJSON[~/.ledit/config.json]
    
    MCPEnabled{MCP Enabled?} -->|Yes| MCPConfig[configure-mcp.sh]
    MCPEnabled -->|No| SkipMCP[Skip MCP Setup]
    
    MCPConfig --> MCPConfigJSON[~/.ledit/mcp_config.json]
```

### 3. Agent Execution Flow

```mermaid
flowchart TD
    Context[Issue Context] --> Prompt[Prompt Builder]
    Images[Downloaded Images] --> Prompt
    UserInstr[User Instructions] --> Prompt
    MCPTools[MCP Tools Info] --> Prompt
    
    Prompt --> Agent[Ledit Agent]
    Agent --> Analyze[Analyze Issue]
    Analyze --> Vision{Images Present?}
    
    Vision -->|Yes| AnalyzeImages[Analyze Images with Vision]
    Vision -->|No| PlanChanges[Plan Changes]
    
    AnalyzeImages --> PlanChanges
    PlanChanges --> Implement[Implement Solution]
    
    Implement --> Validate[Validate Changes]
    Validate -->|Issues Found| Retry[Self-Correct & Retry]
    Validate -->|Success| Complete[Complete]
    
    Retry --> Implement
```

## Key Decision Points

### Trigger Detection
- `/ledit` command → Process with optional user instructions
- Manual trigger → Process entire issue
- Issue event → Process on creation/update

### Branch Management
- Existing branch → Pull latest changes, handle conflicts
- New branch → Create from default branch

### PR Management
- Existing PR → Add update comment
- New PR → Create with full description

### Error Handling
- Timeout → Report with action logs link
- No changes → Explain possible reasons
- Failure → Provide debugging guidance

## Data Flow Summary

1. **Input**: GitHub issue with description, comments, and images
2. **Processing**: Ledit agent analyzes and implements solution
3. **Output**: Git branch with changes and pull request

## State Management

The action maintains state through:
- Environment variables (GITHUB_ENV)
- Temporary files (/tmp/ledit-issue-*)
- Git branch state
- GitHub Action outputs

## Parallel Operations

Some operations run in parallel for efficiency:
- Image downloads (multiple concurrent)
- API pagination (when supported)
- File operations (minimal blocking)

## Cost Optimization

The action optimizes costs by:
- Configurable timeouts
- Efficient context building
- Reusing existing branches
- Minimal API calls