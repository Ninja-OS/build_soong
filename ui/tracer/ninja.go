// Copyright 2016 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tracer

import (
	"bufio"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type ninjaLogEntry struct {
	Name  string
	Begin int
	End   int
}
type ninjaLogEntries []*ninjaLogEntry

func (n ninjaLogEntries) Len() int           { return len(n) }
func (n ninjaLogEntries) Less(i, j int) bool { return n[i].Begin < n[j].Begin }
func (n ninjaLogEntries) Swap(i, j int)      { n[i], n[j] = n[j], n[i] }

// ImportNinjaLog reads a .ninja_log file from ninja and writes the events out
// to the trace.
//
// startOffset is when the ninja process started, and is used to position the
// relative times from the ninja log into the trace. It's also used to skip
// reading the ninja log if nothing was run.
func (t *tracerImpl) ImportNinjaLog(thread Thread, filename string, startOffset time.Time) {
	t.Begin("ninja log import", thread)
	defer t.End(thread)

	if stat, err := os.Stat(filename); err != nil {
		t.log.Println("Missing ninja log:", err)
		return
	} else if stat.ModTime().Before(startOffset) {
		t.log.Verboseln("Ninja log not modified, not importing any entries.")
		return
	}

	f, err := os.Open(filename)
	if err != nil {
		t.log.Println("Error opening ninja log:", err)
		return
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	header := true
	entries := ninjaLogEntries{}
	prevEnd := 0
	for s.Scan() {
		if header {
			hdr := s.Text()
			if hdr != "# ninja log v5" {
				t.log.Printf("Unknown ninja log header: %q", hdr)
				return
			}
			header = false
			continue
		}

		fields := strings.Split(s.Text(), "\t")
		begin, err := strconv.Atoi(fields[0])
		if err != nil {
			t.log.Printf("Unable to parse ninja entry %q: %v", s.Text(), err)
			return
		}
		end, err := strconv.Atoi(fields[1])
		if err != nil {
			t.log.Printf("Unable to parse ninja entry %q: %v", s.Text(), err)
			return
		}
		if end < prevEnd {
			entries = nil
		}
		prevEnd = end
		entries = append(entries, &ninjaLogEntry{
			Name:  fields[3],
			Begin: begin,
			End:   end,
		})
	}
	if err := s.Err(); err != nil {
		t.log.Println("Unable to parse ninja log:", err)
		return
	}

	sort.Sort(entries)

	cpus := []int{}
	offset := uint64(startOffset.UnixNano()) / 1000
	for _, entry := range entries {
		tid := -1
		for cpu, endTime := range cpus {
			if endTime <= entry.Begin {
				tid = cpu
				cpus[cpu] = entry.End
				break
			}
		}
		if tid == -1 {
			tid = len(cpus)
			cpus = append(cpus, entry.End)
		}

		t.writeEvent(&viewerEvent{
			Name:  entry.Name,
			Phase: "X",
			Time:  offset + uint64(entry.Begin)*1000,
			Dur:   uint64(entry.End-entry.Begin) * 1000,
			Pid:   1,
			Tid:   uint64(tid),
		})
	}
}
