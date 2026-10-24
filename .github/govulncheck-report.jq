# Renders `govulncheck -format json` output as a PR comment, or nothing when no vulnerability is reachable.
# Only symbol-level findings count, matching what govulncheck's text mode reports as affecting the code.
def cell: tostring | gsub("\\|"; "\\|") | gsub("\n"; " ");
def symbol: "`\(.package).\(if .receiver then "\(.receiver)." else "" end)\(.function)`";

(map(.osv // empty | {key: .id, value: .summary}) | from_entries) as $summaries
| [.[].finding // empty | select(.trace[0].function)]
| group_by(.osv)
| select(length > 0)
| map(
    .[0] as $f
    | $f.trace[0] as $vuln
    | "| [\($f.osv)](https://pkg.go.dev/vuln/\($f.osv)) | \($summaries[$f.osv] // "" | cell) | `\($vuln.module)@\($vuln.version)` | \($f.fixed_version // "not fixed") | \(map(.trace[0] | symbol) | unique | join("<br>")) |"
  ) as $rows
| ([
    "### govulncheck: \($rows | length) reachable \(if ($rows | length) == 1 then "vulnerability" else "vulnerabilities" end)",
    "",
    "<details>",
    "<summary>Findings</summary>",
    "",
    "| ID | Summary | Module | Fixed in | Vulnerable symbols called |",
    "|---|---|---|---|---|"
  ] + $rows + ["", "</details>"])
| join("\n")
