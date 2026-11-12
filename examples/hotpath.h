/* hotpath.h: search the buffered request body in place. No copy into Go. */
#ifndef HOTPATH_H
#define HOTPATH_H

#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

/* body_contains returns 1 when the request body of `stream` contains the
 * `n`-byte `pattern`, and 0 otherwise. It reads the buffered body, then the
 * body received in the current hook, as one byte sequence, so a match may span
 * chunks. `stream` is the envoy_dynamic_module_type_http_filter_envoy_ptr.
 * Call it only on the stream's owner thread, during a body callback. */
int body_contains(void* stream, const char* pattern, size_t n);

#ifdef __cplusplus
}
#endif
#endif
