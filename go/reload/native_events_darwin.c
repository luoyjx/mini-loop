//go:build darwin && cgo

#include "native_events_darwin.h"
#include <dispatch/dispatch.h>
#include <stdlib.h>
#include <string.h>
struct MiniLoopEvents {
 FSEventStreamRef stream;
 dispatch_queue_t queue;
 Boolean started;
};
static void mini_loop_start_on_queue(void *info) {
 MiniLoopEvents *events=info;
 FSEventStreamSetDispatchQueue(events->stream,events->queue);
 events->started=FSEventStreamStart(events->stream);
}
static void mini_loop_stop_on_queue(void *info) {
 MiniLoopEvents *events=info;
 if (events->started) FSEventStreamStop(events->stream);
 // Invalidate unschedules the stream; never detach its queue before this call.
 FSEventStreamInvalidate(events->stream);
}
static void mini_loop_queue_barrier(void *info) {}
extern void goMiniLoopNativeEvent(uintptr_t key, char *path, uint32_t flags);
static void mini_loop_callback(ConstFSEventStreamRef stream, void *info,
 size_t count, void *paths, const FSEventStreamEventFlags flags[],
 const FSEventStreamEventId ids[]) {
 char **names = paths;
 for (size_t i=0;i<count;i++) goMiniLoopNativeEvent((uintptr_t)info,names[i],flags[i]);
}
MiniLoopEvents *mini_loop_events_start(uintptr_t key, const char *roots) {
 CFMutableArrayRef paths=CFArrayCreateMutable(NULL,0,&kCFTypeArrayCallBacks);
 if (!paths) return NULL;
 const char *cursor=roots;
 while (*cursor) {
  CFStringRef path=CFStringCreateWithCString(NULL,cursor,kCFStringEncodingUTF8);
  if (!path) { CFRelease(paths); return NULL; }
  CFArrayAppendValue(paths,path); CFRelease(path);
  cursor += strlen(cursor)+1;
 }
 FSEventStreamContext context={0,(void*)key,NULL,NULL,NULL};
 FSEventStreamRef stream=FSEventStreamCreate(NULL,mini_loop_callback,&context,
  paths,kFSEventStreamEventIdSinceNow,0.0,
  kFSEventStreamCreateFlagFileEvents|kFSEventStreamCreateFlagNoDefer);
 CFRelease(paths);
 if (!stream) return NULL;
 MiniLoopEvents *events=calloc(1,sizeof(*events));
 if (!events) { FSEventStreamRelease(stream); return NULL; }
 events->stream=stream;
 events->queue=dispatch_queue_create("mini-loop.fsevents",DISPATCH_QUEUE_SERIAL);
 if (!events->queue) { FSEventStreamRelease(stream); free(events); return NULL; }
 dispatch_sync_f(events->queue,events,mini_loop_start_on_queue);
 if (!events->started) { mini_loop_events_close(events); return NULL; }
 return events;
}
void mini_loop_events_close(MiniLoopEvents *events) {
 // Control operations and callbacks share one serial queue. Stop/invalidate
 // joins active callbacks; the second barrier drains work queued before detach.
 dispatch_sync_f(events->queue,events,mini_loop_stop_on_queue);
 dispatch_sync_f(events->queue,NULL,mini_loop_queue_barrier);
 FSEventStreamRelease(events->stream);
 dispatch_release(events->queue);
 free(events);
}
