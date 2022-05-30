// Code generated for package assets by go-bindata DO NOT EDIT. (@generated)
// sources:
// templates/args.html
// templates/collection/item.html
// templates/collection/list.html
// templates/collection/main.html
// templates/env.html
// templates/mem.html
// templates/metrics.html
// templates/proc.html
// templates/scopes.html
// templates/signals.html
// templates/version.html
package assets

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"
)
type asset struct {
	bytes []byte
	info  os.FileInfo
}

type bindataFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
}

// Name return file name
func (fi bindataFileInfo) Name() string {
	return fi.name
}

// Size return file size
func (fi bindataFileInfo) Size() int64 {
	return fi.size
}

// Mode return file mode
func (fi bindataFileInfo) Mode() os.FileMode {
	return fi.mode
}

// Mode return file modify time
func (fi bindataFileInfo) ModTime() time.Time {
	return fi.modTime
}

// IsDir return file whether a directory
func (fi bindataFileInfo) IsDir() bool {
	return fi.mode&os.ModeDir != 0
}

// Sys return file is sys mode
func (fi bindataFileInfo) Sys() interface{} {
	return nil
}

var _templatesArgsHtml = []byte(`{{ define "content" }}

<p>
    The set of command-line arguments used when starting this process.
</p>

<table>
    <thead>
    <tr>
        <th>Index</th>
        <th>Value</th>
    </tr>
    </thead>

    <tbody>
        {{ range $index, $value := . }}
            <tr>
                <td>{{$index}}</td>
                <td>{{$value}}</td>
            </tr>
        {{ end }}
    </tbody>
</table>

<br>

{{ template "last-refresh" .}}
{{ end }}
`)

func templatesArgsHtmlBytes() ([]byte, error) {
	return _templatesArgsHtml, nil
}

func templatesArgsHtml() (*asset, error) {
	bytes, err := templatesArgsHtmlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "templates/args.html", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _templatesCollectionItemHtml = []byte(`{{ define "content" }}

{{ with $context := . }}
    {{ if ne $context.Error "" }}
        <b>{{$context.Error}}</b>
    {{else}}
        <p> Item {{ $context.Collection }}/{{ $context.Key }}</p>
        <div class="language-yaml highlighter-rouge">
            <div class="highlight">
                <pre class="highlight"><code>{{ $context.Value }}</code></pre>
            </div>
        </div>
    {{end}}
{{end}}
{{ template "last-refresh" .}}

{{ end }}
`)

func templatesCollectionItemHtmlBytes() ([]byte, error) {
	return _templatesCollectionItemHtml, nil
}

func templatesCollectionItemHtml() (*asset, error) {
	bytes, err := templatesCollectionItemHtmlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "templates/collection/item.html", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _templatesCollectionListHtml = []byte(`{{ define "content" }}

{{ with $context := . }}

    {{ if ne $context.Error "" }}
        <b>{{$context.Error}}</b>
    {{else}}
        <p> Collection {{ $context.Collection }} </p>

        <table>
            <thead>
            <tr>
                <th>Index</th>
                <th>Item</th>
            </tr>
            </thead>

            <tbody>
                 {{ range $index, $key := $context.Keys }}
                <tr>
                    <td>{{$index}}</td>
                    <td><a href="{{$context.Collection}}/{{$key}}">{{$key}}</a></td>
                </tr>
                {{ end }}
            </tbody>
        </table>
    {{end}}

{{end}}

{{ template "last-refresh" .}}

{{ end }}
`)

func templatesCollectionListHtmlBytes() ([]byte, error) {
	return _templatesCollectionListHtml, nil
}

func templatesCollectionListHtml() (*asset, error) {
	bytes, err := templatesCollectionListHtmlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "templates/collection/list.html", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _templatesCollectionMainHtml = []byte(`{{ define "content" }}

{{ with $context := .}}
    <p>Collections</p>

    <table>
        <tbody>
        {{ range $context.Collections }}
            <tr>
                <td><a href="{{.}}">{{.}}</a></td>
            </tr>
        {{ end }}
        </tbody>
    </table>
{{end}}

{{ template "last-refresh" .}}

{{ end }}
`)

func templatesCollectionMainHtmlBytes() ([]byte, error) {
	return _templatesCollectionMainHtml, nil
}

func templatesCollectionMainHtml() (*asset, error) {
	bytes, err := templatesCollectionMainHtmlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "templates/collection/main.html", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _templatesEnvHtml = []byte(`{{ define "content" }}

<p>
    The set of environment variables defined for this process.
</p>

<table>
    <thead>
    <tr>
        <th>Name</th>
        <th>Value</th>
    </tr>
    </thead>

    <tbody>
        {{ range . }}
            <tr>
                <td>{{.Name}}</td>
                <td>{{.Value}}</td>
            </tr>
        {{ end }}
    </tbody>
</table>

{{ template "last-refresh" .}}

{{ end }}
`)

func templatesEnvHtmlBytes() ([]byte, error) {
	return _templatesEnvHtml, nil
}

func templatesEnvHtml() (*asset, error) {
	bytes, err := templatesEnvHtmlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "templates/env.html", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _templatesMemHtml = []byte(`{{ define "content" }}

<p>
    This information is gathered from the Go runtime and represents the ongoing memory consumption
    of this process. Please refer to the <a href="https://golang.org/pkg/runtime/#MemStats">Go documentation on the MemStats type</a>
    for more information about all of these values.
</p>

<table>
    <thead>
    <tr>
        <th>Counter</th>
        <th>Value</th>
        <th>Description</th>
    </tr>
    </thead>
    <tbody>
    <tr>
        <td>HeapInuse</td>
        <td id="HeapInuse">{{.HeapInuse}} bytes</td>
        <td>Bytes in in-use spans.</td>
    </tr>

    <tr>
        <td>Total Alloc</td>
        <td id="TotalAlloc">{{.TotalAlloc}} bytes</td>
        <td>Cumulative bytes allocated for heap objects.</td>
    </tr>

    <tr>
        <td>Sys</td>
        <td id="Sys">{{.Sys}} bytes</td>
        <td>Total bytes of memory obtained from the OS.</td>
    </tr>

    <tr>
        <td>Lookups</td>
        <td id="Lookups">{{.Lookups}} lookups</td>
        <td>Number of pointer lookups performed by the runtime.</td>
    </tr>

    <tr>
        <td>Mallocs</td>
        <td id="Mallocs">{{.Mallocs}} objects</td>
        <td>Cumulative count of heap objects allocated.</td>
    </tr>

    <tr>
        <td>Frees</td>
        <td id="Frees">{{.Frees}} objects</td>
        <td>Cumulative count of heap objects freed.</td>
    </tr>

    <tr>
        <td>Live</td>
        <td id="Live">0 objects</td>
        <td>Count of live heap objects.</td>
    </tr>

    <tr>
        <td>HeapAlloc</td>
        <td id="HeapAlloc">{{.HeapAlloc}} bytes</td>
        <td>Allocated heap objects.</td>
    </tr>

    <tr>
        <td>HeapSys</td>
        <td id="HeapSys">{{.HeapSys}} bytes</td>
        <td>Heap memory obtained from the OS.</td>
    </tr>

    <tr>
        <td>HeapIdle</td>
        <td id="HeapIdle">{{.HeapIdle}} bytes</td>
        <td>Bytes in idle (unused) spans.</td>
    </tr>

    <tr>
        <td>HeapReleased</td>
        <td id="HeapReleased">{{.HeapReleased}} bytes</td>
        <td>Physical memory returned to the OS.</td>
    </tr>

    <tr>
        <td>HeapObjects</td>
        <td id="HeapObjects">{{.HeapObjects}} objects</td>
        <td>Number of allocated heap objects.</td>
    </tr>

    <tr>
        <td>StackInuse</td>
        <td id="StackInuse">{{.StackInuse}} bytes</td>
        <td>Bytes in stack spans.</td>
    </tr>

    <tr>
        <td>StackSys</td>
        <td id="StackSys">{{.StackSys}} bytes</td>
        <td>Stack memory obtained from the OS.</td>
    </tr>

    <tr>
        <td>NextGC</td>
        <td id="NextGC">{{.NextGC}} bytes</td>
        <td>Target heap size of the next GC cycle.</td>
    </tr>

    <tr>
        <td>LastGC</td>
        <td id="LastGC"></td>
        <td>The time the last garbage collection finished.</td>
    </tr>

    <script>
        // we do this so there's a useful date in the table, which avoids things shifting around during initial paint
        let d = new Date().toLocaleString();
        document.getElementById("LastGC").innerText = d;
    </script>

    <tr>
        <td>PauseTotalNs</td>
        <td id="PauseTotalNs">{{.PauseTotalNs}} ns</td>
        <td>Cumulative time spent in GC stop-the-world pauses.</td>
    </tr>

    <tr>
        <td>NumGC</td>
        <td id="NumGC">{{.NumGC}} GC cycles</td>
        <td>Completed GC cycles.</td>
    </tr>

    <tr>
        <td>NumForcedGC</td>
        <td id="NumForcedGC">{{.NumForcedGC}} GC cycles</td>
        <td>GC cycles that were forced by the application calling the GC function.</td>
    </tr>

    <tr>
        <td>GCCPUFraction</td>
        <td id="GCCPUFraction"></td>
        <td>Fraction of this program's available CPU time used by the GC since the program started.</td>
    </tr>
    </tbody>
</table>

<br>
<button class="btn btn-istio" onclick="forceCollection()">Force Garbage Collection</button>

{{ template "last-refresh" .}}

<script>
    "use strict";

    function refreshMemStats() {
        let url = window.location.protocol + "//" + window.location.host + "/memj/";

        let ajax = new XMLHttpRequest();
        ajax.onload = onload;
        ajax.onerror = onerror;
        ajax.open("GET", url, true);
        ajax.send();

        function onload() {
            if (this.status === 200) { // request succeeded
                let ms = JSON.parse(this.responseText);
                document.getElementById("HeapInuse").innerText = ms.HeapInuse.toLocaleString() + " bytes";
                document.getElementById("TotalAlloc").innerText = ms.TotalAlloc.toLocaleString() + " bytes";
                document.getElementById("Sys").innerText = ms.Sys.toLocaleString() + " bytes";
                document.getElementById("Lookups").innerText = ms.Lookups.toLocaleString() + " lookups";
                document.getElementById("Mallocs").innerText = ms.Mallocs.toLocaleString() + " objects";
                document.getElementById("Frees").innerText = ms.Frees.toLocaleString() + " objects";
                document.getElementById("Live").innerText = (ms.Mallocs - ms.Frees).toLocaleString() + " objects";
                document.getElementById("HeapAlloc").innerText = ms.HeapAlloc.toLocaleString() + " bytes";
                document.getElementById("HeapSys").innerText = ms.HeapSys.toLocaleString() + " bytes";
                document.getElementById("HeapIdle").innerText = ms.HeapIdle.toLocaleString() + " bytes";
                document.getElementById("HeapReleased").innerText = ms.HeapReleased.toLocaleString() + " bytes";
                document.getElementById("HeapObjects").innerText = ms.HeapObjects.toLocaleString() + " objects";
                document.getElementById("StackInuse").innerText = ms.StackInuse.toLocaleString() + " bytes";
                document.getElementById("StackSys").innerText = ms.StackSys.toLocaleString() + " bytes";
                document.getElementById("NextGC").innerText = ms.NextGC.toLocaleString() + " bytes";
                document.getElementById("PauseTotalNs").innerText = ms.PauseTotalNs.toLocaleString() + " ns";
                document.getElementById("NumGC").innerText = ms.NumGC.toLocaleString() + " GC cycles";
                document.getElementById("NumForcedGC").innerText = ms.NumForcedGC.toLocaleString() + " GC cycles";

                let d = new Date(ms.LastGC / 1000000).toLocaleString();
                document.getElementById("LastGC").innerText = d;

                let frac = ms.GCCPUFraction;
                if (frac < 0) {
                    frac = 0.0;
                }
                let percent = (frac * 100).toFixed(2);
                document.getElementById("GCCPUFraction").innerText = percent + "%";

                updateRefreshTime();
            }
        }

        function onerror(e) {
            console.error(e);
        }
    }

    function forceCollection() {
        let url = window.location.protocol + "//" + window.location.host + "/memj/forcecollection";

        let ajax = new XMLHttpRequest();
        ajax.onload = onload;
        ajax.onerror = onerror;
        ajax.open("PUT", url, true);
        ajax.send();

        function onload() {
        }

        function onerror(e) {
            console.error(e);
        }
    }

    refreshMemStats();
    window.setInterval(refreshMemStats, 1000);

</script>

{{ end }}
`)

func templatesMemHtmlBytes() ([]byte, error) {
	return _templatesMemHtml, nil
}

func templatesMemHtml() (*asset, error) {
	bytes, err := templatesMemHtmlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "templates/mem.html", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _templatesMetricsHtml = []byte(`{{ define "content" }}

<p>
    The set of metrics published by this process.
</p>

<style>
    .metric-card {
        margin-bottom: 4px;
    }

    .metric-header {
        padding: 0.5rem;
        cursor: pointer;
        background-color: #5f6364;
    }

    .metric-body {
        padding: 0;
    }

    .metric-table {
        margin: 0;
    }
</style>

{{ range . }}
    {{ if .Metrics }}
        <div class="card metric-card">
            <div class="card-header metric-header" onclick="$('#{{.Name | normalize}}').collapse('toggle')" role="button" aria-expanded="false" aria-controls="{{.Name}}">
                {{.Name}} - {{.Help}} [{{.Type}}]
            </div>

            <div id="{{.Name | normalize }}" class="collapse">
                <div class="card-body metric-body">
                    <table class="metric-table">
                        {{ if eq .Type "GAUGE" "COUNTER" "UNTYPED" "LASTVALUE" "COUNT" }}
                            <thead>
                                <tr>
                                    <th>Labels</th>
                                    <th>Value</th>
                                </tr>
                            </thead>
                            <tbody>
                                {{ range .Metrics }}
                                    <tr>
                                        <td>
                                            {{ range $k, $v := .Labels }}
                                                {{$k}} : {{$v}}<br>
                                            {{ end }}
                                        </td>
                                        <td>
                                            {{.Value}}
                                        </td>
                                    </tr>
                                {{ end }}
                            </tbody>
                        {{ else }}
                            <thead>
                                <tr>
                                    <th>Labels</th>
                                    <th>Count</th>
                                    <th>Sum</th>
                                </tr>
                            </thead>
                            <tbody>
                                {{ range .Metrics }}
                                    <tr>
                                        <td>
                                            {{ range $k, $v := .Labels }}
                                                {{$k}} : {{$v}}<br>
                                            {{ end }}
                                        </td>
                                        <td>
                                            {{.Count}}
                                        </td>
                                        <td>
                                            {{.Sum}}
                                        </td>
                                    </tr>
                                {{ end }}
                            </tbody>
                        {{ end }}
                    </table>
                </div>
            </div>
        </div>
    {{ end }}
{{ end }}

{{ template "last-refresh" .}}

<script>
    "use strict";

    function refreshMetrics() {
        let url = window.location.protocol + "//" + window.location.host + "/metricj/";

        let ajax = new XMLHttpRequest();
        ajax.onload = onload;
        ajax.onerror = onerror;
        ajax.open("GET", url, true);
        ajax.send();

        function onload() {
            if (this.status === 200) { // request succeeded
                let families = JSON.parse(this.responseText);

                for (let i = 0; i < families.length; i++) {
                    let name = families[i].name;
                    let div = document.getElementById(name);

                    if (div) {
                        let tbody = div.getElementsByTagName("TBODY")[0];
                        for (let j = 0; j < tbody.children.length; j++) {
                            let tr = tbody.children[j];
                            let labels_td = tr.children[0];

                            let labels = "";
                            if (families[i].metrics[j].labels) {
                                for (let key in families[i].metrics[j].labels) {
                                    if (labels.length > 0) {
                                        labels = labels + "<br>";
                                    }

                                    labels = labels + key + " : " + families[i].metrics[j].labels[key];
                                }
                            }

                            labels_td.innerHTML = labels;

                            if (families[i].metrics[j].value !== undefined) {
                                let value_td = tr.children[1];
                                value_td.innerText = families[i].metrics[j].value;
                            } else {
                                let count_td = tr.children[1];
                                let sum_td = tr.children[2];
                                count_td.innerText = families[i].metrics[j].count;
                                sum_td.innerText = families[i].metrics[j].sum;
                            }
                        }
                    }
                }

                updateRefreshTime();
            }
        }

        function onerror(e) {
            console.error(e);
        }
    }

    refreshMetrics();
    window.setInterval(refreshMetrics, 1000);

</script>

{{ end }}
`)

func templatesMetricsHtmlBytes() ([]byte, error) {
	return _templatesMetricsHtml, nil
}

func templatesMetricsHtml() (*asset, error) {
	bytes, err := templatesMetricsHtmlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "templates/metrics.html", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _templatesProcHtml = []byte(`{{ define "content" }}

<p>
    Information about this process.
</p>

<table>
    <thead>
    <tr>
        <th>Name</th>
        <th>Value</th>
    </tr>
    </thead>

    <tbody>
        <tr>
            <td># Threads</td>
            <td id="Threads">{{.Threads}}</td>
        </tr>

        <tr>
            <td># Goroutines</td>
            <td id="Goroutines">{{.Goroutines}}</td>
        </tr>

        <tr>
            <td>Effective Group Id</td>
            <td>{{.Egid}}</td>
        </tr>

        <tr>
            <td>Effective User Id</td>
            <td>{{.Euid}}</td>
        </tr>

        <tr>
            <td>Group Id</td>
            <td>{{.GID}}</td>
        </tr>

        <tr>
            <td>Groups</td>
            <td>{{.Groups}}</td>
        </tr>

        <tr>
            <td>Hostname</td>
            <td>{{.Hostname}}</td>
        </tr>

        <tr>
            <td>Parent Process Id</td>
            <td>{{.Ppid}}</td>
        </tr>

        <tr>
            <td>Process Id</td>
            <td>{{.Pid}}</td>
        </tr>

        <tr>
            <td>Temporary Directory</td>
            <td>{{.TempDir}}</td>
        </tr>

        <tr>
            <td>User Id</td>
            <td>{{.UID}}</td>
        </tr>

        <tr>
            <td>Working Directory</td>
            <td>{{.Wd}}</td>
        </tr>
    </tbody>
</table>

{{ template "last-refresh" .}}

<script>
    "use strict";

    function refreshProcStats() {
        let url = window.location.protocol + "//" + window.location.host + "/procj/";

        let ajax = new XMLHttpRequest();
        ajax.onload = onload;
        ajax.onerror = onerror;
        ajax.open("GET", url, true);
        ajax.send();

        function onload() {
            if (this.status === 200) { // request succeeded
                let pi = JSON.parse(this.responseText);
                document.getElementById("Threads").innerText = pi.threads;
                document.getElementById("Goroutines").innerText = pi.goroutines;

                updateRefreshTime();
            }
        }

        function onerror(e) {
            console.error(e);
        }
    }

    refreshProcStats();
    window.setInterval(refreshProcStats, 1000);

</script>

{{ end }}
`)

func templatesProcHtmlBytes() ([]byte, error) {
	return _templatesProcHtml, nil
}

func templatesProcHtml() (*asset, error) {
	bytes, err := templatesProcHtmlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "templates/proc.html", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _templatesScopesHtml = []byte(`{{ define "content" }}
<p>
        Logging for this process is organized in scopes. Each scope has different
        output controls which determine how much and what kind of logging is produced
        by the scope.
</p>

<table>
    <thead>
        <tr>
            <th>Scope</th>
            <th>Description</th>
            <th>Output Level</th>
            <th>Stack Trace Level</th>
            <th>Log Callers?</th>
        </tr>
    </thead>

    <tbody>

        {{ range $index, $value := . }}
            <tr id="{{$value.Name}}">
                <td>{{$value.Name}}</td>
                <td>{{$value.Description}}</td>
                <td class="text-center">
                    <div class="dropdown">
                        <button id="outputLevel" class="btn btn-istio dropdown-toggle" type="button" data-toggle="dropdown">
                            {{$value.OutputLevel}}
                        </button>
                        <div class="dropdown-menu">
                            <a class="dropdown-item" onclick="selectOutputLevel(this, 'none')">none</a>
                            <a class="dropdown-item" onclick="selectOutputLevel(this, 'error')">error</a>
                            <a class="dropdown-item" onclick="selectOutputLevel(this, 'warn')">warn</a>
                            <a class="dropdown-item" onclick="selectOutputLevel(this, 'info')">info</a>
                            <a class="dropdown-item" onclick="selectOutputLevel(this, 'debug')">debug</a>
                        </div>
                    </div>
                </td>

                <td class="text-center">
                    <div class="dropdown">
                        <button id="stackTraceLevel" class="btn btn-istio dropdown-toggle" type="button" data-toggle="dropdown">
                        {{$value.StackTraceLevel}}
                        </button>
                        <div class="dropdown-menu">
                            <a class="dropdown-item" onclick="selectStackTraceLevel(this, 'none')">none</a>
                            <a class="dropdown-item" onclick="selectStackTraceLevel(this, 'error')">error</a>
                            <a class="dropdown-item" onclick="selectStackTraceLevel(this, 'warn')">warn</a>
                            <a class="dropdown-item" onclick="selectStackTraceLevel(this, 'info')">info</a>
                            <a class="dropdown-item" onclick="selectStackTraceLevel(this, 'debug')">debug</a>
                        </div>
                    </div>
                </td>

                <td class="text-center">
                    {{ if $value.LogCallers}}
                        <input id="logCallers" onclick="toggleLogCallers(this)" type="checkbox" checked="checked">
                    {{ else }}
                        <input id="logCallers" onclick="toggleLogCallers(this)" type="checkbox">
                    {{ end }}
                </td>
            </tr>
        {{ end }}

    </tbody>
</table>

{{ template "last-refresh" .}}

<script>
    "use strict";

    function refreshScopes() {
        let url = window.location.protocol + "//" + window.location.host + "/scopej/";

        let ajax = new XMLHttpRequest();
        ajax.onload = onload;
        ajax.onerror = onerror;
        ajax.open("GET", url, true);
        ajax.send();

        function onload() {
            if (this.status === 200) { // request succeeded
                let si = JSON.parse(this.responseText);
                for (let i = 0; i < si.length; i++) {
                    let info = si[i];

                    let tr = document.getElementById(info.name);
                    tr.querySelector("#outputLevel").innerText = info.output_level;
                    tr.querySelector("#stackTraceLevel").innerText = info.stack_trace_level;
                    tr.querySelector("#logCallers").checked = info.log_callers;
                }

                updateRefreshTime();
            }
        }

        function onerror(e) {
            console.error(e);
        }
    }

    function selectOutputLevel(element, level) {
        let scope = element.parentElement.parentElement.parentElement.parentElement.id;

        let url = window.location.protocol + "//" + window.location.host + "/scopej/" + scope;
        let ajax = new XMLHttpRequest();
        ajax.onload = onload;
        ajax.onerror = onerror;
        ajax.open("GET", url, true);
        ajax.send();

        function onload() {
            if (this.status === 200) { // request succeeded
                let si = JSON.parse(this.responseText);
                si.output_level = level;

                let url = window.location.protocol + "//" + window.location.host + "/scopej/" + scope;
                let ajax = new XMLHttpRequest();
                ajax.onload = onload2;
                ajax.onerror = onerror;
                ajax.open("PUT", url, true);
                ajax.send(JSON.stringify(si));
            }

            function onload2() {
                refreshScopes();
            }
        }

        function onerror(e) {
            console.error(e);
        }
    }

    function selectStackTraceLevel(element, level) {
        let scope = element.parentElement.parentElement.parentElement.parentElement.id;

        let url = window.location.protocol + "//" + window.location.host + "/scopej/" + scope;
        let ajax = new XMLHttpRequest();
        ajax.onload = onload;
        ajax.onerror = onerror;
        ajax.open("GET", url, true);
        ajax.send();

        function onload() {
            if (this.status === 200) { // request succeeded
                let si = JSON.parse(this.responseText);
                si.stack_trace_level = level;

                let url = window.location.protocol + "//" + window.location.host + "/scopej/" + scope;
                let ajax = new XMLHttpRequest();
                ajax.onload = onload2;
                ajax.onerror = onerror;
                ajax.open("PUT", url, true);
                ajax.send(JSON.stringify(si));
            }

            function onload2() {
                refreshScopes();
            }
        }

        function onerror(e) {
            console.error(e);
        }
    }

    function toggleLogCallers(checkbox) {
        let scope = checkbox.parentElement.parentElement.id;
        let logCallers = checkbox.checked;

        let url = window.location.protocol + "//" + window.location.host + "/scopej/" + scope;
        let ajax = new XMLHttpRequest();
        ajax.onload = onload;
        ajax.onerror = onerror;
        ajax.open("GET", url, true);
        ajax.send();

        function onload() {
            if (this.status === 200) { // request succeeded
                let si = JSON.parse(this.responseText);
                si.log_callers = logCallers;

                let url = window.location.protocol + "//" + window.location.host + "/scopej/" + scope;
                let ajax = new XMLHttpRequest();
                ajax.onload = onload2;
                ajax.onerror = onerror;
                ajax.open("PUT", url, true);
                ajax.send(JSON.stringify(si));
            }

            function onload2() {
            }
        }

        function onerror(e) {
            console.error(e);
        }
    }

    refreshScopes();
    window.setInterval(refreshScopes, 1000);
</script>

{{ end }}
`)

func templatesScopesHtmlBytes() ([]byte, error) {
	return _templatesScopesHtml, nil
}

func templatesScopesHtml() (*asset, error) {
	bytes, err := templatesScopesHtmlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "templates/scopes.html", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _templatesSignalsHtml = []byte(`{{ define "content" }}

<p>
    Send signals to the running process.
</p>

<br>
<button class="btn btn-istio" onclick="sendSIGUSR1()">SIGUSR1</button>

{{ template "last-refresh" .}}

<script>
    "use strict";

    function sendSIGUSR1() {
        let url = window.location.protocol + "//" + window.location.host + "/signalj/SIGUSR1";

        let ajax = new XMLHttpRequest();
        ajax.onload = onload;
        ajax.onerror = onerror;
        ajax.open("PUT", url, true);
        ajax.send();

        function onload() {
            console.log(url + " -> " + ajax.status)
        }

        function onerror(e) {
            console.error(e);
        }
    }
</script>

{{ end }}
`)

func templatesSignalsHtmlBytes() ([]byte, error) {
	return _templatesSignalsHtml, nil
}

func templatesSignalsHtml() (*asset, error) {
	bytes, err := templatesSignalsHtmlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "templates/signals.html", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _templatesVersionHtml = []byte(`{{ define "content" }}

<p>
    Version information about this binary and runtime.
</p>

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Value</th>
        </tr>
    </thead>

    <tbody>
        <tr>
            <td>Version</td>
            <td>{{.Version}}</td>
        </tr>

        <tr>
            <td>Git Revision</td>
            <td>{{.GitRevision}}</td>
        </tr>

        <tr>
            <td>GolangVersion</td>
            <td>{{.GolangVersion}}</td>
        </tr>

        <tr>
            <td>Build Status</td>
            <td>{{.BuildStatus}}</td>
        </tr>
    </tbody>
</table>

{{ template "last-refresh" .}}

{{ end }}
`)

func templatesVersionHtmlBytes() ([]byte, error) {
	return _templatesVersionHtml, nil
}

func templatesVersionHtml() (*asset, error) {
	bytes, err := templatesVersionHtmlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "templates/version.html", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

// Asset loads and returns the asset for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func Asset(name string) ([]byte, error) {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[cannonicalName]; ok {
		a, err := f()
		if err != nil {
			return nil, fmt.Errorf("Asset %s can't read by error: %v", name, err)
		}
		return a.bytes, nil
	}
	return nil, fmt.Errorf("Asset %s not found", name)
}

// MustAsset is like Asset but panics when Asset would return an error.
// It simplifies safe initialization of global variables.
func MustAsset(name string) []byte {
	a, err := Asset(name)
	if err != nil {
		panic("asset: Asset(" + name + "): " + err.Error())
	}

	return a
}

// AssetInfo loads and returns the asset info for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func AssetInfo(name string) (os.FileInfo, error) {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[cannonicalName]; ok {
		a, err := f()
		if err != nil {
			return nil, fmt.Errorf("AssetInfo %s can't read by error: %v", name, err)
		}
		return a.info, nil
	}
	return nil, fmt.Errorf("AssetInfo %s not found", name)
}

// AssetNames returns the names of the assets.
func AssetNames() []string {
	names := make([]string, 0, len(_bindata))
	for name := range _bindata {
		names = append(names, name)
	}
	return names
}

// _bindata is a table, holding each asset generator, mapped to its name.
var _bindata = map[string]func() (*asset, error){
	"templates/args.html":            templatesArgsHtml,
	"templates/collection/item.html": templatesCollectionItemHtml,
	"templates/collection/list.html": templatesCollectionListHtml,
	"templates/collection/main.html": templatesCollectionMainHtml,
	"templates/env.html":             templatesEnvHtml,
	"templates/mem.html":             templatesMemHtml,
	"templates/metrics.html":         templatesMetricsHtml,
	"templates/proc.html":            templatesProcHtml,
	"templates/scopes.html":          templatesScopesHtml,
	"templates/signals.html":         templatesSignalsHtml,
	"templates/version.html":         templatesVersionHtml,
}

// AssetDir returns the file names below a certain
// directory embedded in the file by go-bindata.
// For example if you run go-bindata on data/... and data contains the
// following hierarchy:
//     data/
//       foo.txt
//       img/
//         a.png
//         b.png
// then AssetDir("data") would return []string{"foo.txt", "img"}
// AssetDir("data/img") would return []string{"a.png", "b.png"}
// AssetDir("foo.txt") and AssetDir("notexist") would return an error
// AssetDir("") will return []string{"data"}.
func AssetDir(name string) ([]string, error) {
	node := _bintree
	if len(name) != 0 {
		cannonicalName := strings.Replace(name, "\\", "/", -1)
		pathList := strings.Split(cannonicalName, "/")
		for _, p := range pathList {
			node = node.Children[p]
			if node == nil {
				return nil, fmt.Errorf("Asset %s not found", name)
			}
		}
	}
	if node.Func != nil {
		return nil, fmt.Errorf("Asset %s not found", name)
	}
	rv := make([]string, 0, len(node.Children))
	for childName := range node.Children {
		rv = append(rv, childName)
	}
	return rv, nil
}

type bintree struct {
	Func     func() (*asset, error)
	Children map[string]*bintree
}

var _bintree = &bintree{nil, map[string]*bintree{
	"templates": &bintree{nil, map[string]*bintree{
		"args.html": &bintree{templatesArgsHtml, map[string]*bintree{}},
		"collection": &bintree{nil, map[string]*bintree{
			"item.html": &bintree{templatesCollectionItemHtml, map[string]*bintree{}},
			"list.html": &bintree{templatesCollectionListHtml, map[string]*bintree{}},
			"main.html": &bintree{templatesCollectionMainHtml, map[string]*bintree{}},
		}},
		"env.html":     &bintree{templatesEnvHtml, map[string]*bintree{}},
		"mem.html":     &bintree{templatesMemHtml, map[string]*bintree{}},
		"metrics.html": &bintree{templatesMetricsHtml, map[string]*bintree{}},
		"proc.html":    &bintree{templatesProcHtml, map[string]*bintree{}},
		"scopes.html":  &bintree{templatesScopesHtml, map[string]*bintree{}},
		"signals.html": &bintree{templatesSignalsHtml, map[string]*bintree{}},
		"version.html": &bintree{templatesVersionHtml, map[string]*bintree{}},
	}},
}}

// RestoreAsset restores an asset under the given directory
func RestoreAsset(dir, name string) error {
	data, err := Asset(name)
	if err != nil {
		return err
	}
	info, err := AssetInfo(name)
	if err != nil {
		return err
	}
	err = os.MkdirAll(_filePath(dir, filepath.Dir(name)), os.FileMode(0755))
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(_filePath(dir, name), data, info.Mode())
	if err != nil {
		return err
	}
	err = os.Chtimes(_filePath(dir, name), info.ModTime(), info.ModTime())
	if err != nil {
		return err
	}
	return nil
}

// RestoreAssets restores an asset under the given directory recursively
func RestoreAssets(dir, name string) error {
	children, err := AssetDir(name)
	// File
	if err != nil {
		return RestoreAsset(dir, name)
	}
	// Dir
	for _, child := range children {
		err = RestoreAssets(dir, filepath.Join(name, child))
		if err != nil {
			return err
		}
	}
	return nil
}

func _filePath(dir, name string) string {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	return filepath.Join(append([]string{dir}, strings.Split(cannonicalName, "/")...)...)
}
