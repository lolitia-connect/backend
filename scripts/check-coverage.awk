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
function status(value) { return value == 0 ? "PASS" : "FAIL" }
END {
    testStatus = ENVIRON["GITHUB_TEST_STATUS"] + 0
    coverageError = ENVIRON["GITHUB_COVERAGE_ERROR"]
    if (invalid || !total) {
        result = "FAIL"
        message = coverageError != "" ? coverageError : "No usable coverage data"
        print message
        if (ENVIRON["GITHUB_STEP_SUMMARY"] != "") {
            print "## Coverage" >> ENVIRON["GITHUB_STEP_SUMMARY"]
            printf "| Check | Result |\n| --- | --- |\n| Tests | %s |\n| Generated Ent sources | Excluded |\n| Statement coverage | %s |\n", status(testStatus), message >> ENVIRON["GITHUB_STEP_SUMMARY"]
            close(ENVIRON["GITHUB_STEP_SUMMARY"])
        }
        exit 1
    }
    percent = 100 * covered / total
    printf "Covered %d/%d statements (%.4f%%), required %d%%\n", covered, total, percent, minimum
    result = 100 * covered < minimum * total ? "FAIL" : "PASS"
    if (ENVIRON["GITHUB_STEP_SUMMARY"] != "") {
        print "## Coverage" >> ENVIRON["GITHUB_STEP_SUMMARY"]
        printf "| Check | Result |\n| --- | --- |\n| Tests | %s |\n| Generated Ent sources | Excluded |\n| Statement coverage | %s, %.4f%% (%d/%d statements), required %d%% |\n", status(testStatus), result, percent, covered, total, minimum >> ENVIRON["GITHUB_STEP_SUMMARY"]
        close(ENVIRON["GITHUB_STEP_SUMMARY"])
    }
    exit (testStatus != 0 || 100 * covered < minimum * total)
}
