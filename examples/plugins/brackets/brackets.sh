#!/bin/sh
# Selkit plugin: the text comes on stdin, the action in SELKIT_ACTION; the result goes to stdout.
# Exit code 3 means "does not fit this text" (the message goes to stderr).
text=$(cat; printf x)
text=${text%x}

case "$SELKIT_ACTION" in
round) printf '(%s)' "$text" ;;
square) printf '[%s]' "$text" ;;
curly) printf '{%s}' "$text" ;;
unwrap)
	# The first and last characters, spaces around the text aside, have to be a matching pair.
	t=$(printf '%s' "$text" | sed -E '1s/^[[:space:]]+//; $s/[[:space:]]+$//')
	first=$(printf '%s' "$t" | cut -c1)
	last=$(printf '%s' "$t" | tail -c 1)
	case "$first$last" in
	'()' | '[]' | '{}') printf '%s' "$t" | sed -E '1s/^.//; $s/.$//' ;;
	*)
		echo "The text is not in brackets" >&2
		exit 3
		;;
	esac
	;;
*) echo "Unknown action $SELKIT_ACTION" >&2; exit 1 ;;
esac
