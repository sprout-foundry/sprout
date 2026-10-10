#!/bin/sh
# Entrypoint for the public sprout workspace image (sprout runner container
# mode). It prepares /workspace and execs the sprout daemon on 0.0.0.0:56000;
# the runner publishes that port on the host's loopback only.
#
# Two kinds of workspace:
#   - WORKSPACE_DIR set: /workspace is the user's own directory, bind-mounted
#     by the runner. It is served in place; nothing is cloned into it or
#     removed from it.
#   - otherwise /workspace is a runner-managed volume. When it holds no
#     repository yet and REPO_URL is set, the repository is cloned into it.
#
# The clone never deletes anything it did not create. It starts only when
# /workspace is empty (lost+found aside), clones into a staging directory
# and then moves the checkout in, keeping a marker that lists what it moved
# so a clone interrupted by a restart can be cleaned up and retried. Any
# other content stops the container with an error instead.
set -eu

root=${SPROUT_WORKSPACE_ROOT:-/workspace}
port=56000
marker_name=.sprout-clone-pending
staging_name=.sprout-clone-staging
marker="$root/$marker_name"
staging="$root/$staging_name"

log() { printf 'sprout-workspace: %s\n' "$*" >&2; }
fatal() {
    log "error: $*"
    exit 1
}

configure_git() {
    # HOME is a tmpfs in the container, so this global config lives only as
    # long as the workspace runs and never reaches the image or the volume.
    git config --global credential.helper sprout
    # The bind-mounted directory is usually owned by the host user, whose
    # uid need not be 1000; git would otherwise refuse to work in it.
    git config --global --add safe.directory "$root"
    if [ -z "$(git config --global user.name || true)" ]; then
        git config --global user.name "${SPROUT_GIT_USER_NAME:-sprout}"
    fi
    if [ -z "$(git config --global user.email || true)" ]; then
        git config --global user.email "${SPROUT_GIT_USER_EMAIL:-sprout@localhost}"
    fi
}

# listed reports whether name is one of the entries a previous clone moved
# into the workspace (recorded in the marker before the move started).
listed() {
    [ -f "$marker" ] && grep -Fqx -- "$1" "$marker"
}

# check_contents refuses to go on when the workspace holds anything other
# than lost+found and, after an interrupted clone, the clone's own files.
check_contents() {
    for entry in "$root"/* "$root"/.[!.]* "$root"/..?*; do
        [ -e "$entry" ] || [ -L "$entry" ] || continue
        name=${entry##*/}
        case "$name" in
            lost+found) continue ;;
        esac
        if [ -f "$marker" ]; then
            case "$name" in
                "$marker_name" | "$staging_name") continue ;;
            esac
            if listed "$name"; then
                continue
            fi
        fi
        fatal "$root is not empty and has no git repository (found '$name'); refusing to clone into it. Remove its contents or use a fresh workspace."
    done
}

# recover_interrupted_clone removes what an earlier, unfinished clone left:
# its staging directory and the entries it had already moved in.
recover_interrupted_clone() {
    [ -f "$marker" ] || return 0
    log "cleaning up an interrupted clone"
    rm -rf "$staging"
    while IFS= read -r name; do
        case "$name" in
            "" | . | .. | */* | lost+found | "$marker_name" | "$staging_name") continue ;;
        esac
        rm -rf "${root:?}/$name"
    done <"$marker"
    rm -f "$marker"
}

clone_repo() {
    case "$REPO_URL" in
        https://?*) ;;
        *) fatal "refusing to clone '$REPO_URL': only https repositories are supported" ;;
    esac
    check_contents
    recover_interrupted_clone

    log "cloning $REPO_URL"
    : >"$marker"
    GIT_TERMINAL_PROMPT=0 git clone -- "$REPO_URL" "$staging"

    # Record every top-level entry before moving any of it, so a restart
    # mid-move can tell the clone's files apart from anything else.
    ls -A "$staging" >"$marker.tmp"
    mv "$marker.tmp" "$marker"
    while IFS= read -r name; do
        [ "$name" = .git ] && continue
        if [ -e "$root/$name" ] || [ -L "$root/$name" ]; then
            fatal "cannot move '$name' into $root: an entry with that name already exists"
        fi
        mv "$staging/$name" "$root/$name"
    done <"$marker"
    # .git goes last: its presence means the checkout is complete.
    mv "$staging/.git" "$root/.git"
    rmdir "$staging"
    rm -f "$marker"
}

prepare_workspace() {
    [ -d "$root" ] || fatal "$root does not exist; the workspace must be mounted there"
    if [ -n "${WORKSPACE_DIR:-}" ]; then
        log "serving the local directory in place"
        return 0
    fi
    if [ -d "$root/.git" ]; then
        # A restart between moving .git in and removing the marker.
        if [ -f "$marker" ]; then
            rm -rf "$staging"
            rm -f "$marker"
        fi
        return 0
    fi
    if [ -n "${REPO_URL:-}" ]; then
        clone_repo
    fi
}

case "${SPROUT_MODE:-daemon}" in
    daemon) ;;
    *) fatal "unsupported SPROUT_MODE '${SPROUT_MODE}'; this image runs the sprout daemon only" ;;
esac

configure_git
prepare_workspace
cd "$root"
log "starting the sprout daemon on 0.0.0.0:$port"
exec sprout agent --daemon --web-port "$port" --bind 0.0.0.0
