"""Produce a private C build overlay without editing the production package.

Usage: .venv/bin/python FILE OUTPUT_DIRECTORY
Then: cd go && go build -overlay OUTPUT_DIRECTORY/overlay.json -o BINARY PROBE.go
"""
import hashlib
import json
import sys
from pathlib import Path


def overlay(destination):
    root = Path(__file__).resolve().parents[2]
    source = root / 'go/reload/native_events_darwin.c'
    original = source.read_text()
    includes = '#include <string.h>\n'
    callback = ' for (size_t i=0;i<count;i++) goMiniLoopNativeEvent((uintptr_t)info,names[i],flags[i]);'
    startup = 'MiniLoopEvents *mini_loop_events_start(uintptr_t key, const char *roots) {\n'
    assert original.count(includes) == original.count(callback) == original.count(startup) == 1
    instrumented = original.replace(includes, includes+'#include <stdio.h>\n#include <time.h>\n')
    instrumented = instrumented.replace(callback, ''' for (size_t i=0;i<count;i++) {
  struct timespec now;
  clock_gettime(CLOCK_REALTIME,&now);
  fprintf(stderr,"MINILOOP_RAWC\\t%llu\\t%lu\\t%llu\\t%u\\t%s\\n",
   (unsigned long long)now.tv_sec*1000000000ULL+(unsigned long long)now.tv_nsec,
   (unsigned long)(uintptr_t)info,(unsigned long long)ids[i],(unsigned int)flags[i],names[i]);
  goMiniLoopNativeEvent((uintptr_t)info,names[i],flags[i]);
 }''')
    instrumented = instrumented.replace(startup, startup+' fprintf(stderr,"MINILOOP_RAW_START\\t%lu\\t%s\\n",(unsigned long)key,roots);\n')
    target = Path(destination).resolve()
    target.mkdir(mode=0o700)
    backing = target / source.name
    backing.write_text(instrumented)
    (target / 'overlay.json').write_text(json.dumps({'Replace': {str(source): str(backing)}}, indent=2)+'\n')
    (target / 'hashes.json').write_text(json.dumps({
        'production_c': hashlib.sha256(source.read_bytes()).hexdigest(),
        'instrumented_c': hashlib.sha256(backing.read_bytes()).hexdigest(),
        'production_source': str(source), 'instrumented_source': instrumented,
    }, indent=2)+'\n')


if __name__ == '__main__':
    overlay(sys.argv[1])
