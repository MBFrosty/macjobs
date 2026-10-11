#!/bin/sh
# Fail if tracked files or this PR's commit subjects look personal.
set -eu

# Allowlist (explicit):
#   Home directory example: /Users/alice
#   Email domains: example.com, example.org, users.noreply.github.com
#   Tailnet hostname: example-host.tailnet.ts.net
# Tracker product hosts are assembled at runtime so this file does not
# itself contain those hostnames as a single token.

allow_user=alice
allow_email_domains='example.com example.org users.noreply.github.com'
allow_ts_host=example-host.tailnet.ts.net

root=$(git rev-parse --show-toplevel)
cd "$root"

hits=$(mktemp)
trap 'rm -f "$hits"' EXIT

report() {
	printf '%s\n' "$1" >&2
	printf '%s\n' "$1" >>"$hits"
}

git_grep() {
	git grep -nI -E "$1" -- ':!go.sum' || true
}

email_ok() {
	domain=${1##*@}
	for allowed in $allow_email_domains; do
		if [ "$domain" = "$allowed" ]; then
			return 0
		fi
	done
	return 1
}

users_pat=$(printf '/%s/%s' Users '[^/[:space:]]+')
users_sed=$(printf 's|.*/%s/\\([^/[:space:]]*\\).*|\\1|p' Users)
git_grep "$users_pat" | while IFS= read -r line; do
	[ -n "$line" ] || continue
	file=${line%%:*}
	rest=${line#*:}
	lineno=${rest%%:*}
	user=$(printf '%s\n' "$line" | sed -n "$users_sed")
	if [ "$user" != "$allow_user" ]; then
		report "$file:$lineno: home path is not the allowlisted example"
	fi
done

git_grep '[A-Za-z0-9._%+\-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}' | while IFS= read -r line; do
	[ -n "$line" ] || continue
	file=${line%%:*}
	rest=${line#*:}
	lineno=${rest%%:*}
	printf '%s\n' "$line" | grep -oE '[A-Za-z0-9._%+\-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}' | while IFS= read -r addr; do
		if ! email_ok "$addr"; then
			report "$file:$lineno: email domain is not allowlisted"
		fi
	done
done

git_grep '[A-Za-z0-9._-]+\.ts\.net' | while IFS= read -r line; do
	[ -n "$line" ] || continue
	file=${line%%:*}
	rest=${line#*:}
	lineno=${rest%%:*}
	printf '%s\n' "$line" | grep -oE '[A-Za-z0-9._-]+\.ts\.net' | while IFS= read -r host; do
		if [ "$host" != "$allow_ts_host" ]; then
			report "$file:$lineno: tailnet host is not the allowlisted example"
		fi
	done
done

check_tracker() {
	needle=$(printf '%s.%s' "$1" "$2")
	git grep -nI -F "$needle" -- ':!go.sum' || true
}

check_tracker linear app | while IFS= read -r line; do
	[ -n "$line" ] || continue
	file=${line%%:*}
	rest=${line#*:}
	lineno=${rest%%:*}
	report "$file:$lineno: tracker host is forbidden"
done

check_tracker atlassian net | while IFS= read -r line; do
	[ -n "$line" ] || continue
	file=${line%%:*}
	rest=${line#*:}
	lineno=${rest%%:*}
	report "$file:$lineno: tracker host is forbidden"
done

if git rev-parse --verify origin/main >/dev/null 2>&1; then
	git log --format='%h %s' origin/main..HEAD | while IFS= read -r line; do
		[ -n "$line" ] || continue
		hash=${line%% *}
		subject=${line#* }
		if printf '%s\n' "$subject" | grep -qE '^[A-Z]{2,6}-[0-9]+'; then
			report "commit $hash: subject starts with an issue key"
		fi
	done
fi

if [ -s "$hits" ]; then
	exit 1
fi
printf 'check-public: ok\n'
