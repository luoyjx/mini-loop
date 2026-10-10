#include <stdint.h>
#include <CoreServices/CoreServices.h>
typedef struct MiniLoopEvents MiniLoopEvents;
MiniLoopEvents *mini_loop_events_start(uintptr_t key, const char *roots);
void mini_loop_events_run(MiniLoopEvents *events);
void mini_loop_events_request_stop(MiniLoopEvents *events);
void mini_loop_events_close(MiniLoopEvents *events);
