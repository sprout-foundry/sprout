#!/bin/bash

# ledit Version Manager Script
# Simplified version management for tag-based deployment process

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Get current git tag
get_current_tag() {
    git describe --tags --abbrev=0 2>/dev/null || echo ""
}

# Get current git commit hash
get_current_commit() {
    git rev-parse --short HEAD 2>/dev/null || echo ""
}

# Get build timestamp
get_build_date() {
    date -u +"%Y-%m-%dT%H:%M:%SZ"
}

# Build with version information
build_with_version() {
    echo -e "${BLUE}Building ledit with version information...${NC}"
    
    local tag=$(get_current_tag)
    local commit=$(get_current_commit)
    local date=$(get_build_date)
    
    # If no tag, use commit hash as version
    if [ -z "$tag" ]; then
        tag="dev-$commit"
    fi
    
    local ldflags="-X 'github.com/alantheprice/ledit/cmd.version=$tag'"
    ldflags="$ldflags -X 'github.com/alantheprice/ledit/cmd.gitCommit=$commit'"
    ldflags="$ldflags -X 'github.com/alantheprice/ledit/cmd.buildDate=$date'"
    ldflags="$ldflags -X 'github.com/alantheprice/ledit/cmd.gitTag=$tag'"
    
    echo -e "${GREEN}Using ldflags: $ldflags${NC}"
    
    go build -ldflags "$ldflags" -o ledit .
    
    echo -e "${GREEN}Build completed successfully!${NC}"
    echo -e "${BLUE}Version information:${NC}"
    ./ledit version
}

# Show current version information
show_version_info() {
    echo -e "${BLUE}Current Version Information:${NC}"
    echo "Version: $(get_current_tag)"
    echo "Git Commit: $(get_current_commit)"
    echo "Build Date: $(get_build_date)"
}

# Generate ldflags for CI/CD systems
generate_ldflags() {
    local tag=$(get_current_tag)
    local commit=$(get_current_commit)
    local date=$(get_build_date)
    
    # If no tag, use commit hash as version
    if [ -z "$tag" ]; then
        tag="dev"
    fi
    
    echo "-X 'github.com/alantheprice/ledit/cmd.version=$tag'"
    echo "-X 'github.com/alantheprice/ledit/cmd.gitCommit=$commit'"
    echo "-X 'github.com/alantheprice/ledit/cmd.buildDate=$date'"
    echo "-X 'github.com/alantheprice/ledit/cmd.gitTag=$tag'"
}

# Print usage information
usage() {
    echo "Usage: $0 [command]"
    echo ""
    echo "Commands:"
    echo "  build    - Build ledit with version information"
    echo "  show     - Show current version information"
    echo "  ldflags  - Generate ldflags for CI/CD systems"
    echo ""
    echo "Examples:"
    echo "  $0 build    # Build with version info"
    echo "  $0 show     # Show current version info"
    echo ""
    echo "For GitHub Actions integration:"
    echo "  LD_FLAGS=\$(./scripts/version-manager.sh ldflags | tr '\\n' ' ')"
    echo "  go build -ldflags \"\$LD_FLAGS\" -o ledit ."
}

# Main execution
main() {
    local command="$1"
    
    case "$command" in
        "build")
            build_with_version
            ;;
        "show")
            show_version_info
            ;;
        "ldflags")
            generate_ldflags
            ;;
        "" | "-h" | "--help")
            usage
            ;;
        *)
            echo -e "${RED}Unknown command: $command${NC}"
            usage
            exit 1
            ;;
    esac
}

# Run main function
main "$@"