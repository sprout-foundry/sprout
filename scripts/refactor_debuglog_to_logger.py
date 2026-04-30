
#!/usr/bin/env python3
"""
Replace a.debugLog(...) with a.Logger().Debug/Info/Warn/Error(...).

Strategy:
- Process file as a list of lines
- For each line containing .debugLog(, extract the complete call
  (may span multiple lines by counting parens)
- Parse the receiver, format string, args using character-level scanning
  on just the relevant portion (not the whole file)
- Replace in-place

Classification:
  "[DEBUG]" or "DEBUG:" -> Debug()
  "[WARN]" -> Warn()
  "[!!]" or "[signal]" -> Warn()
  "[OK]" or "[+]" -> Info()
  "Warning:" / "Warning " -> Warn()
  "Failed" -> Error()
  "DEFER:" -> Debug()
  Everything else -> Debug()
"""
import sys
import os


def find_format_string_end(s, start):
    """
    Given string s and start position just after opening quote of format string,
    find the position of the closing quote. Handles escaped quotes.
    Returns index of closing quote, or -1.
    """
    i = start
    while i < len(s):
        if s[i] == '\\':
            i += 2
            continue
        if s[i] == '"':
            return i
        i += 1
    return -1


def find_matching_paren(s, start):
    """
    Find matching closing paren for the opening paren at position start.
    Handles string literals with escaped quotes.
    Returns index or -1.
    """
    depth = 0
    i = start
    in_string = False
    while i < len(s):
        c = s[i]
        if in_string:
            if c == '\\':
                i += 2
                continue
            if c == '"':
                in_string = False
            i += 1
            continue
        if c == '"':
            in_string = True
            i += 1
            continue
        if c == '(':
            depth += 1
        elif c == ')':
            depth -= 1
            if depth == 0:
                return i
        else:
            i += 1
    return -1


def classify_level(format_str):
    s = format_str.strip()
    if s.startswith('[WARN]'):
        return 'Warn', s[6:].lstrip(' ')
    elif s.startswith('[!!]'):
        return 'Warn', s[4:].lstrip(' ')
    elif s.startswith('[signal]'):
        return 'Warn', s[8:].lstrip(' ')
    elif s.startswith('[OK]'):
        return 'Info', s[4:].lstrip(' ')
    elif s.startswith('[+]'):
        return 'Info', s[3:].lstrip(' ')
    elif s.startswith('[DEBUG]'):
        return 'Debug', s[7:].lstrip(' ')
    elif s.startswith('DEBUG:'):
        return 'Debug', s[6:].lstrip(' ')
    elif s.startswith('Warning:') or s.startswith('Warning '):
        return 'Warn', s[8:].lstrip(' ')
    elif s.startswith('Failed'):
        return 'Error', s
    elif s.startswith('DEFER:'):
        return 'Debug', s[6:].lstrip(' ')
    return 'Debug', s


def clean_format(f):
    while f.endswith('\\n'):
        f = f[:-2]
    return f.strip()


def process_line_with_debuglog(full_call_text):
    """
    Given the full text of a debugLog call (may span multiple lines joined with \n),
    parse and rewrite it.
    Returns rewritten text or None if can't parse.
    """
    # Find .debugLog(
    idx = full_call_text.find('.debugLog(')
    if idx == -1:
        return None

    receiver = full_call_text[:idx]
    after = full_call_text[idx + len('.debugLog('):]

    # Skip whitespace before opening quote
    q_start = 0
    while q_start < len(after) and after[q_start] in ' \t\n':
        q_start += 1

    if q_start >= len(after) or after[q_start] != '"':
        return None

    # Find closing quote
    q_end = find_format_string_end(after, q_start + 1)
    if q_end == -1:
        return None

    format_str = after[q_start+1:q_end]

    # Find matching closing paren from after the opening paren of debugLog(
    open_paren_idx = idx + len('.debugLog')  # position of '('
    close_paren = find_matching_paren(full_call_text, open_paren_idx)
    if close_paren == -1:
        return None

    rest = after[q_end+1:close_paren - idx - len('.debugLog(') + 1]

    level, cleaned = classify_level(format_str)
    cleaned = clean_format(cleaned)

    return f'{receiver}.Logger().{level}("{cleaned}"{rest}'


def process_file(filepath, dry_run=True):
    with open(filepath, 'r') as f:
        lines = f.readlines()

    original_lines = list(lines)
    replacements = 0
    i = 0

    while i < len(lines):
        line = lines[i]
        if '.debugLog(' not in line:
            i += 1
            continue

        # This line has debugLog( - find complete call (may span lines)
        call_start = i
        joined = line
        # Count parens after .debugLog(
        d_idx = line.find('.debugLog(')
        open_count = line[d_idx:].count('(')
        close_count = line[d_idx:].count(')')

        while open_count > close_count and i + 1 < len(lines):
            i += 1
            joined += lines[i]
            open_count += lines[i].count('(')
            close_count += lines[i].count(')')

        # Now process the joined text
        result = process_line_with_debuglog(joined)
        if result is not None:
            # Replace lines[call_start..i] with result
            lines[call_start:i+1] = [result + '\n']
            replacements += 1

        i = call_start + 1  # move past the replaced block

    new_content = ''.join(lines)

    if replacements == 0:
        return 0

    if dry_run:
        print(f"\n=== {filepath} ({replacements} replacements) ===")
        old = original_lines
        new = new_content.split('\n')
        changed = 0
        for j in range(max(len(old), len(new))):
            old_l = old[j].rstrip('\n') if j < len(old) else ''
            new_l = new[j] if j < len(new) else ''
            if old_l != new_l and changed < 15:
                if '.Logger()' in new_l or '.debugLog(' in old_l:
                    print(f"  L{j+1}: {old_l[:120]}")
                    print(f"  ->      {new_l[:120]}")
                    changed += 1
        if changed >= 15:
            print("  ...")
        print()
        return replacements
    else:
        with open(filepath, 'w') as f:
            f.write(new_content)
        print(f"  {filepath}: {replacements}")
        return replacements


def main():
    target = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), 'pkg', 'agent')

    excludes = {'agent_debug.go', 'agent_logger.go', 'utils.go', 'scripted_client.go', 'agent.go'}
    total = 0
    dry = '--dry-run' in sys.argv

    for fname in sorted(os.listdir(target)):
        if not fname.endswith('.go') or fname in excludes or fname.endswith('_test.go'):
            continue
        path = os.path.join(target, fname)
        with open(path) as f:
            content = f.read()
        if '.debugLog(' not in content:
            continue
        has_calls = any(
            '.debugLog(' in line
            and not line.strip().startswith('func ')
            and not line.strip().startswith('//')
            for line in content.split('\n')
        )
        if not has_calls:
            continue
        total += process_file(path, dry_run=dry)

    print(f"\nTotal: {total} replacements" + (" (dry run)" if dry else ""))


if __name__ == '__main__':
    main()
