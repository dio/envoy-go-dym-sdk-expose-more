/* plugin.h: a native plugin for a Go plugin host. It knows nothing about Go.
 * The host passes the stream pointer and, at config time, the config pointer.
 * The plugin uses the ABI callbacks directly. */
#ifndef PLUGIN_H
#define PLUGIN_H

#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

/* plugin_config_init defines a counter named `name` on the filter config
 * `config`, the envoy_dynamic_module_type_http_filter_config_envoy_ptr, and
 * stores its id in `*id`. It returns 0 on success. Call it while the config is
 * created. */
int plugin_config_init(void* config, const char* name, size_t* id);

/* plugin_on_response_headers sets the response header `x-native-plugin` on
 * `stream` and adds 1 to the counter `id` of `config`. Call it on the
 * stream's owner thread, during the response headers callback. */
void plugin_on_response_headers(void* stream, void* config, size_t id);

#ifdef __cplusplus
}
#endif
#endif
