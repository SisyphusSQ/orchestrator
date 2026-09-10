/*
   Copyright 2014 Outbrain Inc.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package inst

import (
	"errors"
	"regexp"
	"strings"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
)

// Event entries may contains table IDs (can be different for same tables on different servers)
// and also COMMIT transaction IDs (different values on different servers).
// So these need to be removed from the event entry if we're to compare and validate matching
// entries.
var eventInfoTransformations map[*regexp.Regexp]string = map[*regexp.Regexp]string{
	regexp.MustCompile(`(.*) [/][*].*?[*][/](.*$)`):  "$1 $2",         // strip comments
	regexp.MustCompile(`(COMMIT) .*$`):               "$1",            // commit number varies cross servers
	regexp.MustCompile(`(table_id:) [0-9]+ (.*$)`):   "$1 ### $2",     // table ids change cross servers
	regexp.MustCompile(`(table_id:) [0-9]+$`):        "$1 ###",        // table ids change cross servers
	regexp.MustCompile(` X'([0-9a-fA-F]+)' COLLATE`): " 0x$1 COLLATE", // different ways to represent collate
	regexp.MustCompile(`(BEGIN GTID [^ ]+) cid=.*`):  "$1",            // MariaDB GTID someimtes gets addition of "cid=...". Stripping
}

var skippedEventTypes map[string]bool = map[string]bool{
	"Format_desc": true,
	"Stop":        true,
	"Rotate":      true,
}

type BinlogEvent struct {
	Coordinates  BinlogCoordinates
	NextEventPos int64
	EventType    string
	Info         string
}

func (event *BinlogEvent) NextBinlogCoordinates() BinlogCoordinates {
	return BinlogCoordinates{LogFile: event.Coordinates.LogFile, LogPos: event.NextEventPos, Type: event.Coordinates.Type}
}

func (event *BinlogEvent) NormalizeInfo() {
	for reg, replace := range eventInfoTransformations {
		event.Info = reg.ReplaceAllString(event.Info, replace)
	}
}

func (event *BinlogEvent) Equals(other *BinlogEvent) bool {
	return event.Coordinates.Equals(&other.Coordinates) &&
		event.NextEventPos == other.NextEventPos &&
		event.EventType == other.EventType && event.Info == other.Info
}

func (event *BinlogEvent) EqualsIgnoreCoordinates(other *BinlogEvent) bool {
	return event.NextEventPos == other.NextEventPos &&
		event.EventType == other.EventType && event.Info == other.Info
}

const maxEmptyEventsEvents int = 10

type BinlogEventCursor struct {
	cachedEvents      []BinlogEvent
	currentEventIndex int
	fetchNextEvents   func(BinlogCoordinates) ([]BinlogEvent, error)
	nextCoordinates   BinlogCoordinates
}

// fetchNextEventsFunc expected to return events starting at a given position, and automatically fetch those from next
// binary log when no more rows are found in current log.
// It is expected to return empty array with no error upon end of binlogs
// It is expected to return error upon error...
func NewBinlogEventCursor(startCoordinates BinlogCoordinates, fetchNextEventsFunc func(BinlogCoordinates) ([]BinlogEvent, error)) BinlogEventCursor {
	events, _ := fetchNextEventsFunc(startCoordinates)
	var initialNextCoordinates BinlogCoordinates
	if len(events) > 0 {
		initialNextCoordinates = events[0].NextBinlogCoordinates()
	}
	return BinlogEventCursor{
		cachedEvents:      events,
		currentEventIndex: -1,
		fetchNextEvents:   fetchNextEventsFunc,
		nextCoordinates:   initialNextCoordinates,
	}
}

// nextEvent will return the next event entry from binary logs; it will automatically skip to next
// binary log if need be.
// Internally, it uses the cachedEvents array, so that it does not go to the MySQL server upon each call.
// Returns nil upon reaching end of binary logs.
func (cursor *BinlogEventCursor) nextEvent(numEmptyEventsEvents int) (*BinlogEvent, error) {
	if numEmptyEventsEvents > maxEmptyEventsEvents {
		log.Debugf("End of logs. currentEventIndex: %d, nextCoordinates: %+v", cursor.currentEventIndex, cursor.nextCoordinates)
		// End of logs
		return nil, nil
	}
	if len(cursor.cachedEvents) == 0 {
		// Cache exhausted; get next bulk of entries and return the next entry
		nextFileCoordinates, err := cursor.nextCoordinates.NextFileCoordinates()
		if err != nil {
			return nil, err
		}
		log.Debugf("zero cached events, next file: %+v", nextFileCoordinates)
		cursor.cachedEvents, err = cursor.fetchNextEvents(nextFileCoordinates)
		if err != nil {
			return nil, err
		}
		cursor.currentEventIndex = -1
		// While this seems recursive do note that recursion level is at most 1, since we either have
		// entries in the next binlog (no further recursion) or we don't (immediate termination)
		return cursor.nextEvent(numEmptyEventsEvents + 1)
	}
	if cursor.currentEventIndex+1 < len(cursor.cachedEvents) {
		// We have enough cache to go by
		cursor.currentEventIndex++
		event := &cursor.cachedEvents[cursor.currentEventIndex]
		cursor.nextCoordinates = event.NextBinlogCoordinates()
		return event, nil
	} else {
		// Cache exhausted; get next bulk of entries and return the next entry
		var err error
		cursor.cachedEvents, err = cursor.fetchNextEvents(cursor.cachedEvents[len(cursor.cachedEvents)-1].NextBinlogCoordinates())
		if err != nil {
			return nil, err
		}
		cursor.currentEventIndex = -1
		// While this seems recursive do note that recursion level is at most 1, since we either have
		// entries in the next binlog (no further recursion) or we don't (immediate termination)
		return cursor.nextEvent(numEmptyEventsEvents + 1)
	}
}

// NextRealEvent returns the next event from binlog that is not meta/control event (these are start-of-binary-log,
// rotate-binary-log etc.)
func (cursor *BinlogEventCursor) nextRealEvent(recursionLevel int) (*BinlogEvent, error) {
	if recursionLevel > maxEmptyEventsEvents {
		log.Debugf("End of real events")
		return nil, nil
	}
	event, err := cursor.nextEvent(0)
	if err != nil {
		return event, err
	}
	if event == nil {
		return event, err
	}

	if _, found := skippedEventTypes[event.EventType]; found {
		// Recursion will not be deep here. A few entries (end-of-binlog followed by start-of-bin-log) are possible,
		// but we really don't expect a huge sequence of those.
		return cursor.nextRealEvent(recursionLevel + 1)
	}
	for _, skipSubstring := range config.Config.PseudoGTID.SkipBinlogContaining {
		if strings.Contains(event.Info, skipSubstring) {
			// Recursion might go deeper here.
			return cursor.nextRealEvent(recursionLevel + 1)
		}
	}
	event.NormalizeInfo()
	return event, err
}

// NextCoordinates return the binlog coordinates of the next entry as yet unprocessed by the cursor.
// Moreover, when the cursor terminates (consumes last entry), these coordinates indicate what will be the futuristic
// coordinates of the next binlog entry.
// The value of this function is used by match-below to move a replica behind another, after exhausting the shared binlog
// entries of both.
func (cursor *BinlogEventCursor) getNextCoordinates() (BinlogCoordinates, error) {
	if cursor.nextCoordinates.LogPos == 0 {
		return cursor.nextCoordinates, errors.New("next coordinates unfound")
	}
	return cursor.nextCoordinates, nil
}
