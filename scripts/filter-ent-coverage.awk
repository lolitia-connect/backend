FILENAME == ARGV[1] {
    generated[module "/" $0] = 1
    next
}
FNR == 1 { print; next }
{
    source = $1
    sub(/:[0-9]+\.[0-9]+,[0-9]+\.[0-9]+$/, "", source)
    if (!(source in generated)) print
}
