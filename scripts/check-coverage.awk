NR == 1 && /^mode: (set|count|atomic)$/ { next }
NF != 3 || $2 !~ /^[0-9]+$/ || $3 !~ /^[0-9]+$/ { invalid = 1; exit }
{
    # Multiple package test binaries can report the same source block.
    if (!($1 in seen)) {
        seen[$1] = 1
        total += $2
    }
    if ($3 > 0 && !($1 in hit)) {
        hit[$1] = 1
        covered += $2
    }
}
END {
    if (invalid || !total) {
        print "No usable coverage data"
        exit 1
    }
    printf "Covered %d/%d statements (%.4f%%), required %d%%\n", covered, total, 100 * covered / total, minimum
    exit (100 * covered < minimum * total)
}
