#!/bin/sh
# Stand-in for a long-lived claude with --input-format stream-json. For each stdin line it prints
# one result line saying which process answered (its pid), how many lines that process has read,
# its arguments and the message. Total cost is cumulative, 0.01 per line, as claude reports it.
# A message starting with "die" makes the process exit without a result. It exits when stdin closes.
# A control_request line is not a message: it is remembered and shown in the next answer as ctl=.
n=0
ctl=
while IFS= read -r line; do
	case "$line" in *'"control_request"'*)
		sub=$(printf '%s' "$line" | sed -n 's/.*"subtype":"\([a-z_]*\)".*/\1/p')
		val=$(printf '%s' "$line" | sed -n 's/.*"\(mode[l]*\)":"\([^"]*\)".*/\2/p')
		ctl="$ctl$sub=$val;"
		continue ;;
	esac
	n=$((n+1))
	text=$(printf '%s' "$line" | sed -n 's/.*"content":"\(.*\)"}}$/\1/p')
	case "$text" in die*) exit 3 ;; esac
	cost=$(awk "BEGIN{print $n*0.01}")
	printf '{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"pid=%s n=%s args=%s ctl=%s text=%s"}]},"session_id":"s-conv"}\n' "$$" "$n" "$*" "$ctl" "$text"
	printf '{"type":"result","subtype":"success","is_error":false,"result":"pid=%s n=%s args=%s ctl=%s text=%s","session_id":"s-conv","total_cost_usd":%s,"usage":{"input_tokens":1,"cache_read_input_tokens":%s,"output_tokens":1}}\n' "$$" "$n" "$*" "$ctl" "$text" "$cost" "$n"
done
