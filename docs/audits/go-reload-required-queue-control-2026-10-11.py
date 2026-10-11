"""Observe the same native queue and post-result control without widening tests.

Original operation assertions run first. Cleanup then writes a distinct control
and collects for a separate three-second diagnostic window before original Close.
The timer and cleanup overlay alter scheduling: never claim temporal acceptance.
"""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import time

ROOT = Path(__file__).resolve().parents[2]


def load(name):
    path = Path(__file__).with_name(name)
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def classify(log):
    original = load("go-reload-required-callbacks-2026-10-11.py").classify(log)
    roots, ticks, items, starts, ends, operations = {}, {}, [], {}, {}, {}
    for line in log.splitlines():
        fields = line.split("\t")
        if fields[0] == "MINILOOP_RAW_START":
            roots[fields[2]] = int(fields[1])
        elif fields[0] == "MINILOOP_QUEUE_TICK":
            ticks.setdefault(int(fields[2]), []).append(int(fields[1]))
        elif fields[0] == "MINILOOP_RAWC":
            items.append((int(fields[1]), int(fields[2]), fields[5]))
        elif fields[0] == "MINILOOP_CONTROL_BEGIN":
            starts[fields[2]] = (int(fields[1]), fields[3])
        elif fields[0] == "MINILOOP_CONTROL_END":
            ends[fields[2]] = int(fields[1])
        elif fields[0] == "MINILOOP_OP":
            operations[fields[2]] = fields[3]
    original_by_name = {case["name"]: case for case in original["cases"]}
    cases = []
    for name, (start, path) in starts.items():
        key, end = roots[str(Path(path).parent)], ends[name]
        observed = ticks.get(key, [])
        assert observed == sorted(observed), "wall clock order changed; do not classify gaps"
        windows = {"post_result_control": (start, end)}
        if observed:
            windows["first_tick_to_result"] = (observed[0], start)
        if name in original_by_name:
            case = original_by_name[name]
            windows["original_operation"] = (case["operation_start_ns"], case["cleanup_entry_ns"])
        queue = {}
        for phase, (begin, finish) in windows.items():
            marks = [mark for mark in observed if begin <= mark <= finish]
            points = [begin, *marks, finish]
            queue[phase] = {"ticks": len(marks), "max_observed_gap_ms": round(max(b-a for a,b in zip(points,points[1:]))/1e6,3)}
        control = [event for event in items if event[1] == key and start <= event[0] <= end and event[2] == path]
        late = [event for event in items if event[1] == key and start <= event[0] <= end and event[2] == operations.get(name)]
        cases.append({"name": name, "reached_operation": name in original_by_name,
                      "original": original_by_name.get(name), "queue": queue,
                      "post_result_control_path": path, "post_result_control_c_items": len(control),
                      "post_result_control_c_delay_seconds": round((control[0][0]-start)/1e9,6) if control else None,
                      "late_business_c_items_in_control_window": len(late),
                      "control_window_seconds": round((end-start)/1e9,6)})
    return {"cases": cases, "readiness_failures": original["readiness_failures"],
            "scope": "Same queue and distinct post-result control, not concurrent operation/control, untouched-source acceptance, kernel cause or statistical reliability. Diagnostic timer/cleanup alter scheduling."}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--output-prefix", default="/tmp/mini-loop-reload-required-queue-control-2026-10-11")
    prefix = Path(parser.parse_args().output_prefix)
    receipt = Path(str(prefix)+".json")
    assert not receipt.exists(), "choose fresh prefix; do not retry a completed cohort"
    replace = load("go-reload-required-callbacks-2026-10-11.py").replace_once
    helper = load("go-reload-boundary-overlay-2026-10-11.py")
    paths = [ROOT/"go/reload/native_events_darwin.c", ROOT/"go/reload/native_events_recipes_test.go"]
    original = {str(p): p.read_text() for p in paths}
    with tempfile.TemporaryDirectory(prefix="mini-loop-required-queue-control-") as scratch:
        destination = Path(scratch)/"c-overlay"
        helper.overlay(destination)
        c_path = destination/paths[0].name
        c = replace(c_path.read_text(), " Boolean started;", " Boolean started;\n dispatch_source_t diagnostic_timer;")
        c = replace(c, "static void mini_loop_start_on_queue", '''static void diagnostic_tick(void *context) {
 struct timespec now;
 clock_gettime(CLOCK_REALTIME,&now);
 fprintf(stderr,"MINILOOP_QUEUE_TICK\\t%llu\\t%lu\\n",
  (unsigned long long)now.tv_sec*1000000000ULL+(unsigned long long)now.tv_nsec,
  (unsigned long)*(uintptr_t*)context);
}
static void diagnostic_timer_free(void *context) { free(context); }
static void mini_loop_start_on_queue''')
        c = replace(c, " MiniLoopEvents *events=info;\n if (events->started)", " MiniLoopEvents *events=info;\n if (events->diagnostic_timer) dispatch_source_cancel(events->diagnostic_timer);\n if (events->started)")
        c = replace(c, " char **names = paths;", ''' char **names = paths;
 struct timespec entered;
 clock_gettime(CLOCK_REALTIME,&entered);
 fprintf(stderr,"MINILOOP_C_BATCH\\t%llu\\t%lu\\t%lu\\n",
  (unsigned long long)entered.tv_sec*1000000000ULL+(unsigned long long)entered.tv_nsec,
  (unsigned long)(uintptr_t)info,(unsigned long)count);''')
        c = replace(c, " return events;", ''' uintptr_t *tick_key=malloc(sizeof(*tick_key));
 if (tick_key) {
  *tick_key=key;
  events->diagnostic_timer=dispatch_source_create(DISPATCH_SOURCE_TYPE_TIMER,0,0,events->queue);
  if (events->diagnostic_timer) {
   dispatch_set_context(events->diagnostic_timer,tick_key);
   dispatch_source_set_event_handler_f(events->diagnostic_timer,diagnostic_tick);
   dispatch_source_set_cancel_handler_f(events->diagnostic_timer,diagnostic_timer_free);
   dispatch_source_set_timer(events->diagnostic_timer,dispatch_time(DISPATCH_TIME_NOW,100000000),100000000,0);
   dispatch_resume(events->diagnostic_timer);
  } else free(tick_key);
 }
 return events;''')
        c = replace(c, " FSEventStreamRelease(events->stream);", " if (events->diagnostic_timer) dispatch_release(events->diagnostic_timer);\n FSEventStreamRelease(events->stream);")
        c_path.write_text(c)
        test = replace(original[str(paths[1])], "\t\t\tt.Cleanup(func() {", '''\t\t\tt.Cleanup(func() {
\t\t\t\tfmt.Fprintf(os.Stderr,"MINILOOP_END\\t%d\\t%s\\t%s\\n",time.Now().UnixNano(),recipe.Name,sourcePath)
\t\t\t\tcontrolPath := filepath.Join(root,".reload_control.go")
\t\t\t\tfmt.Fprintf(os.Stderr,"MINILOOP_CONTROL_BEGIN\\t%d\\t%s\\t%s\\n",time.Now().UnixNano(),recipe.Name,controlPath)
\t\t\t\tif err := os.WriteFile(controlPath,[]byte("control\\n"),0600); err != nil { t.Error(err) }
\t\t\t\tcontrolContext,controlCancel := context.WithTimeout(context.Background(),3*time.Second)
\t\t\t\tfor {
\t\t\t\t\t_,err := observer.Next(controlContext)
\t\t\t\t\tif err != nil { break }
\t\t\t\t}
\t\t\t\tcontrolCancel()
\t\t\t\tfmt.Fprintf(os.Stderr,"MINILOOP_CONTROL_END\\t%d\\t%s\\t%s\\n",time.Now().UnixNano(),recipe.Name,controlPath)''')
        test = replace(test, '\t\t\ttrace.mark(fmt.Sprintf("operation=%s path=%s", recipe.Operation, sourcePath))', '''\t\t\tfmt.Fprintf(os.Stderr,"MINILOOP_OP\\t%d\\t%s\\t%s\\n",time.Now().UnixNano(),recipe.Name,sourcePath)
\t\t\ttrace.mark(fmt.Sprintf("operation=%s path=%s", recipe.Operation, sourcePath))''')
        test_path = Path(scratch)/paths[1].name
        test_path.write_text(test)
        subprocess.run(["gofmt","-w",str(test_path)],check=True)
        generated = {str(paths[0]): c_path, str(paths[1]): test_path}
        overlay = Path(scratch)/"overlay.json"
        overlay.write_text(json.dumps({"Replace":{p:str(q) for p,q in generated.items()}}))
        command = ["go","test","-overlay="+str(overlay),"-v","-race","-covermode=atomic","-coverpkg=./...",
                   "-count=1","-timeout=180s","-run=^TestNativeEventsActualSourceRecipes/(ignored-hidden|ignored-backup|ignored-text|transient-source)$","./reload"]
        log = Path(str(prefix)+".log")
        began = time.monotonic()
        with log.open("w") as output:
            result = subprocess.run(command,cwd=ROOT/"go",stdout=output,stderr=subprocess.STDOUT)
        report = {"baseline":subprocess.check_output(["git","rev-parse","HEAD"],cwd=ROOT,text=True).strip(),
                  "command":command,"exit_code":result.returncode,"seconds":round(time.monotonic()-began,3),
                  "log":str(log),"raw_log":log.read_text(),
                  "classification":classify(log.read_text()),
                  "production_sha256":{str(p.relative_to(ROOT)):hashlib.sha256(original[str(p)].encode()).hexdigest() for p in paths},
                  "overlay_sources":{str(Path(p).relative_to(ROOT)):q.read_text() for p,q in generated.items()},
                  "production_unchanged":all(p.read_text()==original[str(p)] for p in paths)}
    assert report["production_unchanged"]
    receipt.write_text(json.dumps(report,indent=2)+'\n')
    print("go",result.returncode,flush=True)


if __name__ == "__main__":
    main()
