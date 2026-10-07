#!/bin/sh
# Bash/POSIX Shell Example

echo "=== Bash / POSIX Shell Language Pack ==="
echo "Kernel / Arch : $(uname -s -m)"
echo "Current Date  : $(date -u)"
echo "Counting files in workspace:"
for item in /work/*; do
    if [ -e "$item" ]; then
        echo "  - $(basename "$item")"
    fi
done
echo "Status: OK"
