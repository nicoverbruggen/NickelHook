/* Build with the host C compiler, not the Kobo cross compiler. */
#define _POSIX_C_SOURCE 200809L
#include <errno.h>
#include <fcntl.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

static const char *resource_name(const char *path) {
    const char *slash = strrchr(path, '/');
    return slash ? slash + 1 : path;
}

static int valid_name(const char *name) {
    if (!*name || !strcmp(name, ".") || !strcmp(name, "..")) return 0;
    for (const unsigned char *p = (const unsigned char *)name; *p; p++)
        if (!((*p >= 'a' && *p <= 'z') || (*p >= 'A' && *p <= 'Z') ||
              (*p >= '0' && *p <= '9') || *p == '.' || *p == '_' || *p == '-')) return 0;
    return 1;
}

static int embed(FILE *output, const char *path, int index, size_t *size) {
    /* Reject FIFOs and devices before reading, rather than hanging a build. */
    int fd = open(path, O_RDONLY | O_NONBLOCK);
    if (fd < 0) return -1;
    struct stat info;
    if (fstat(fd, &info) || !S_ISREG(info.st_mode)) {
        close(fd);
        errno = EINVAL;
        return -1;
    }
    FILE *input = fdopen(fd, "rb");
    if (!input) { int error = errno; close(fd); errno = error; return -1; }
    fprintf(output, "static const unsigned char nh_resource_%d[] = {\n", index);
    *size = 0;
    int byte;
    while ((byte = fgetc(input)) != EOF) {
        if (*size == SIZE_MAX) { fclose(input); errno = EOVERFLOW; return -1; }
        fprintf(output, *size % 16 ? " %d," : "    %d,", byte);
        if (++*size % 16 == 0) fputc('\n', output);
    }
    int failed = ferror(input);
    if (fclose(input)) failed = 1;
    if (failed) { errno = EIO; return -1; }
    if (*size % 16) fputc('\n', output);
    fputs("    0};\n", output);
    return 0;
}

int main(int argc, char **argv) {
    if (argc < 2) {
        fprintf(stderr, "Usage: %s OUTPUT [RESOURCE ...]\n", argv[0]);
        return 1;
    }
    for (int i = 2; i < argc; i++) {
        const char *name = resource_name(argv[i]);
        if (!valid_name(name)) {
            fprintf(stderr, "Invalid resource name: %s\n", name);
            return 1;
        }
        for (int j = 2; j < i; j++) {
            if (!strcmp(name, resource_name(argv[j]))) {
                fprintf(stderr, "Duplicate resource name: %s\n", name);
                return 1;
            }
        }
    }
    size_t *sizes = calloc((size_t)argc, sizeof(*sizes));
    char *temporary = malloc(strlen(argv[1]) + sizeof(".tmp.XXXXXX"));
    if (!sizes || !temporary) { perror("allocate resource metadata"); free(sizes); free(temporary); return 1; }
    sprintf(temporary, "%s.tmp.XXXXXX", argv[1]);
    int fd = mkstemp(temporary);
    if (fd < 0) { perror(temporary); free(temporary); free(sizes); return 1; }
    FILE *output = fdopen(fd, "wb");
    int result = 1;
    if (!output) { perror(temporary); close(fd); goto done; }
    fputs("/* Generated from mod resources. */\n#include \"resources.h\"\n", output);
    for (int i = 2; i < argc; i++) {
        if (embed(output, argv[i], i - 2, &sizes[i])) { perror(argv[i]); goto close_output; }
    }
    fputs("static const struct nh_resource nh_embedded_resources[] = {\n", output);
    for (int i = 2; i < argc; i++)
        fprintf(output, "    {\"%s\", nh_resource_%d, %zu},\n", resource_name(argv[i]), i - 2, sizes[i]);
    fputs("    {NULL, NULL, 0}};\n", output);
    /* A failed build must preserve the previous header. The replacement must
       also remain readable across host/container UIDs with a restrictive umask. */
    if (ferror(output) || fflush(output) || fchmod(fd, 0644) || fsync(fd)) {
        perror(temporary);
        goto close_output;
    }
    if (fclose(output)) { perror(temporary); output = NULL; goto done; }
    output = NULL;
    if (rename(temporary, argv[1])) { perror(argv[1]); goto done; }
    result = 0;
close_output:
    if (output) fclose(output);
done:
    if (result) unlink(temporary);
    free(temporary);
    free(sizes);
    return result;
}
