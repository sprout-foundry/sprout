#!/bin/bash
set -e

STATUS="$1"

echo "Reporting status to issue #$ISSUE_NUMBER..."

if [ "$STATUS" == "no-changes" ]; then
    MESSAGE="ℹ️ **No changes needed**

I analyzed the issue but didn't find any changes that needed to be made. This could mean:
- The issue might already be resolved
- The request might need clarification
- The changes might be outside my capabilities

Please provide additional context if needed."

else
    MESSAGE="❌ **Failed to process issue**

I encountered an error while trying to solve this issue. This could be due to:
- Complex requirements that need clarification
- Missing dependencies or context
- Technical limitations

**Error Details:**
Check the GitHub Actions run logs for more information: $GITHUB_SERVER_URL/$GITHUB_REPOSITORY/actions/runs/$GITHUB_RUN_ID

**Next Steps:**
1. Review the error logs
2. Provide additional context if needed
3. Consider breaking down the issue into smaller tasks"
fi

# Post status comment
gh issue comment "$ISSUE_NUMBER" --body "$MESSAGE"

echo "Status reported to issue"