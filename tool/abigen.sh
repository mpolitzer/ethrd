#!/bin/sh

# 1. abigen - abigen binary
# 2. json   - combined abi+bin, output from forge
# 3. pkg    - name only, no path, no extension
# 4. out    - output .go file

set -e
T=$(mktemp -d)
trap 'rm -r $T' EXIT

jq .abi < "$2" > "$T/abi"
jq -r .bytecode.object < "$2" > "$T/bin"
$1 --v2 \
	--abi "$T/abi" \
	--bin "$T/bin" \
	--pkg "$3" \
	--out "$4"

# NOTE: should be equivalent to the above, but doesn't seem to work
# abigen \
#	--combined-json $2 \
#	--pkg "$3" \
#	--out "$4"
