#!/usr/bin/awk -f
# Convert `go test -bench -benchmem` output into a readable Markdown table,
# grouping each benchmark by the pipeline phase it measures.
# Usage: go test -run='^$' -bench=. -benchmem ./... | awk -f scripts/benchtable.awk

# phaseOf maps a benchmark name to its pipeline phase. The order of the returned
# label's leading digit (see phaseOrder) controls how rows are sorted.
function phaseOf(name) {
	if (name ~ /RecordDirect/)   return "pure record"
	if (name ~ /RecordWithPane/) return "record with pane"
	if (name ~ /PaneRender/)     return "pane rendering"
	if (name ~ /SVGRender/)      return "svg rendering"
	if (name ~ /Emulator/)       return "emulation (shared)"
	return "other"
}

function phaseRank(phase) {
	if (phase == "pure record")        return 1
	if (phase == "record with pane")   return 2
	if (phase == "pane rendering")     return 3
	if (phase == "svg rendering")      return 4
	if (phase == "emulation (shared)") return 5
	return 6
}

BEGIN { rows = 0 }

/^goos:/   { goos = $2 }
/^goarch:/ { goarch = $2 }
/^cpu:/    { $1 = ""; sub(/^[ \t]+/, ""); cpu = $0 }
/^pkg:/    { pkg = $2; sub(/.*\//, "", pkg) }

/^Benchmark/ {
	name = $1
	sub(/-[0-9]+$/, "", name)     # drop the -GOMAXPROCS suffix
	iters = $2
	ns = "—"; thr = "—"; mem = "—"; alloc = "—"
	for (i = 3; i <= NF; i++) {
		if ($i == "ns/op")          ns = $(i - 1) " ns"
		else if ($i == "MB/s")      thr = $(i - 1) " MB/s"
		else if ($i == "B/op")      mem = $(i - 1) " B"
		else if ($i == "allocs/op") alloc = $(i - 1)
	}
	names[rows] = name; pkgs[rows] = pkg; it[rows] = iters
	nss[rows] = ns; thrs[rows] = thr; mems[rows] = mem; allocs[rows] = alloc
	phases[rows] = phaseOf(name); ranks[rows] = phaseRank(phases[rows])
	rows++
}

END {
	if (rows == 0) {
		print "_No benchmarks were reported._"
		exit
	}
	if (goos != "") {
		caption = "_" goos "/" goarch
		if (cpu != "") caption = caption " · " cpu
		print caption "_\n"
	}
	print "| Phase | Benchmark | Package | Iterations | Time/op | Throughput | Mem/op | Allocs/op |"
	print "|:--|:--|:--|--:|--:|--:|--:|--:|"
	# Print rows grouped by pipeline rank, showing the phase only on its first row.
	for (rank = 1; rank <= 6; rank++) {
		shown = 0
		for (i = 0; i < rows; i++) {
			if (ranks[i] != rank) continue
			label = (shown == 0) ? phases[i] : ""
			printf "| %s | `%s` | %s | %s | %s | %s | %s | %s |\n",
				label, names[i], pkgs[i], it[i], nss[i], thrs[i], mems[i], allocs[i]
			shown++
		}
	}
}
