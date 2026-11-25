FILENAME == ARGV[1] {
    generated[module "/" $0] = 1
    next
}
{
    package = $1
    for (i = 2; i <= NF; i++) {
        if (!(package "/" $i in generated)) {
            print package
            break
        }
    }
}
