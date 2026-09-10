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
	"context"
	"errors"
	"fmt"
	"log/syslog"
	"os"
	"sync"
	"time"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	"github.com/openark/orchestrator/internal/repository/metadata"

	"github.com/openark/orchestrator/internal/observability"
)

// syslogWriter is optional, and defaults to nil (disabled).
var syslogWriter auditSyslogSink
var syslogMutex sync.RWMutex

type auditSyslogSink interface {
	Info(string) error
	Close() error
}

var auditOperationCounter = observability.NewCounter("orchestrator_audit_write_total", "audit.write events")

// EnableSyslogWriter enables, if possible, writes to syslog. These will execute _in addition_ to normal logging
func EnableAuditSyslog() (err error) {
	writer, err := syslog.New(syslog.LOG_ERR, "orchestrator")
	if err != nil {
		return err
	}
	syslogMutex.Lock()
	previousWriter := syslogWriter
	syslogWriter = writer
	syslogMutex.Unlock()
	if previousWriter == nil {
		return nil
	}
	return previousWriter.Close()
}

// CloseAuditSyslog closes and disables the optional audit syslog sink.
func CloseAuditSyslog() error {
	syslogMutex.Lock()
	writer := syslogWriter
	syslogWriter = nil
	syslogMutex.Unlock()
	if writer == nil {
		return nil
	}
	return writer.Close()
}

// AuditOperation creates and writes a new audit entry by given params
func AuditOperation(auditType string, instanceKey *InstanceKey, message string) error {
	if instanceKey == nil {
		instanceKey = &InstanceKey{}
	}
	clusterName := ""
	if instanceKey.Hostname != "" {
		clusterName, _ = GetClusterName(instanceKey)
	}

	auditWritten := false
	if config.Config.Audit.LogFile != "" {
		text := fmt.Sprintf("%s\t%s\t%s\t%d\t[%s]\t%s\t\n", time.Now().Format(log.TimeFormat), auditType, instanceKey.Hostname, instanceKey.Port, clusterName, message)
		if err := appendAuditFile(config.Config.Audit.LogFile, text); err != nil {
			return log.Errore(err)
		}
		auditWritten = true
	}
	if config.Config.Audit.ToBackend {
		err := metadata.WriteAudit(
			context.Background(), auditType, instanceKey.Hostname, instanceKey.Port, clusterName, message,
		)
		if err != nil {
			return log.Errore(err)
		}
	}
	logMessage := fmt.Sprintf("auditType:%s instance:%s cluster:%s message:%s", auditType, instanceKey.DisplayString(), clusterName, message)
	writtenToSyslog, err := writeAuditSyslog(logMessage)
	if err != nil {
		return log.Errore(err)
	}
	if writtenToSyslog {
		auditWritten = true
	}
	if !auditWritten {
		log.Infof("%s", logMessage)
	}
	auditOperationCounter.Add(context.Background(), 1)

	return nil
}

func appendAuditFile(path, text string) (err error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0640)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, file.Close())
	}()
	_, err = file.WriteString(text)
	return err
}

func writeAuditSyslog(message string) (bool, error) {
	syslogMutex.RLock()
	defer syslogMutex.RUnlock()
	if syslogWriter == nil {
		return false, nil
	}
	return true, syslogWriter.Info(message)
}

// ReadRecentAudit returns a list of audit entries order chronologically descending, using page number.
func ReadRecentAudit(instanceKey *InstanceKey, page int) ([]Audit, error) {
	hostname, port, matchInstance := "", 0, instanceKey != nil
	if instanceKey != nil {
		hostname, port = instanceKey.Hostname, instanceKey.Port
	}
	rows, err := metadata.ReadRecentAudit(
		context.Background(), hostname, port, matchInstance, config.AuditPageSize, page*config.AuditPageSize,
	)
	res := make([]Audit, 0, len(rows))
	for _, row := range rows {
		audit := Audit{
			AuditId:        row.ID,
			AuditTimestamp: row.Timestamp,
			AuditType:      row.Type,
		}
		audit.AuditInstanceKey.Hostname = row.Hostname
		audit.AuditInstanceKey.Port = row.Port
		audit.Message = row.Message
		res = append(res, audit)
	}

	if err != nil {
		log.Errore(err)
	}
	return res, err

}

// ExpireAudit removes old rows from the audit table
func ExpireAudit() error {
	return metadata.ExpireAudit(context.Background(), config.Config.Audit.PurgeDays)
}
