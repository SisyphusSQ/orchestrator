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

package log

import (
	"errors"
	"fmt"
	"log/syslog"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// LogLevel indicates the severity of a log entry
type LogLevel int

func (this LogLevel) String() string {
	switch this {
	case FATAL:
		return "FATAL"
	case CRITICAL:
		return "CRITICAL"
	case ERROR:
		return "ERROR"
	case WARNING:
		return "WARNING"
	case NOTICE:
		return "NOTICE"
	case INFO:
		return "INFO"
	case DEBUG:
		return "DEBUG"
	}
	return "unknown"
}

func LogLevelFromString(logLevelName string) (LogLevel, error) {
	switch logLevelName {
	case "FATAL":
		return FATAL, nil
	case "CRITICAL":
		return CRITICAL, nil
	case "ERROR":
		return ERROR, nil
	case "WARNING":
		return WARNING, nil
	case "NOTICE":
		return NOTICE, nil
	case "INFO":
		return INFO, nil
	case "DEBUG":
		return DEBUG, nil
	}
	return 0, fmt.Errorf("Unknown LogLevel name: %+v", logLevelName)
}

const (
	FATAL LogLevel = iota
	CRITICAL
	ERROR
	WARNING
	NOTICE
	INFO
	DEBUG
)

const TimeFormat = "2006-01-02 15:04:05"

// globalLogLevel indicates the global level filter for all logs (only entries with level equals or higher
// than this value will be logged)
var globalLogLevel atomic.Int32
var printStackTrace atomic.Bool

var zapLevel = zap.NewAtomicLevelAt(zap.DebugLevel)
var sugaredLogger = newSugaredLogger()

const (
	internalNoticeLoggerName   = "golib_notice"
	internalCriticalLoggerName = "golib_critical"
)

// syslogWriter is optional, and defaults to nil (disabled)
var syslogLevel atomic.Int32
var syslogMutex sync.RWMutex
var syslogWriter syslogSink
var closeHooksMutex sync.Mutex
var closeHooks []func() error

type syslogSink interface {
	Emerg(string) error
	Crit(string) error
	Err(string) error
	Warning(string) error
	Notice(string) error
	Info(string) error
	Debug(string) error
	Close() error
}

func init() {
	globalLogLevel.Store(int32(DEBUG))
	syslogLevel.Store(int32(ERROR))
}

// SetPrintStackTrace enables/disables dumping the stack upon error logging
func SetPrintStackTrace(shouldPrintStackTrace bool) {
	printStackTrace.Store(shouldPrintStackTrace)
}

// SetLevel sets the global log level. Only entries with level equals or higher than
// this value will be logged
func SetLevel(logLevel LogLevel) {
	globalLogLevel.Store(int32(logLevel))
	zapLevel.SetLevel(toZapLevel(logLevel))
}

// GetLevel returns current global log level
func GetLevel() LogLevel {
	return LogLevel(globalLogLevel.Load())
}

// EnableSyslogWriter enables, if possible, writes to syslog. These will execute _in addition_ to normal logging
func EnableSyslogWriter(tag string) (err error) {
	writer, err := syslog.New(syslog.LOG_ERR, tag)
	if err != nil {
		return err
	}
	syslogMutex.Lock()
	previousWriter := syslogWriter
	syslogWriter = writer
	syslogMutex.Unlock()
	if previousWriter != nil {
		_ = previousWriter.Close()
	}
	return nil
}

// SetSyslogLevel sets the minimal syslog level. Only entries with level equals or higher than
// this value will be logged. However, this is also capped by the global log level. That is,
// messages with lower level than global-log-level will be discarded at any case.
func SetSyslogLevel(logLevel LogLevel) {
	syslogLevel.Store(int32(logLevel))
}

// logFormattedEntry nicely formats and emits a log entry
func logFormattedEntry(logLevel LogLevel, message string, args ...interface{}) string {
	return emit(logLevel, fmt.Sprintf(message, args...))
}

func emit(logLevel LogLevel, message string) string {
	if logLevel > GetLevel() {
		return ""
	}
	// if TZ env variable is set, update the timestamp timezone
	localizedTime := time.Now()
	tzLocation := os.Getenv("TZ")
	if tzLocation != "" {
		location, err := time.LoadLocation(tzLocation)
		if err == nil { // if invalid tz location was provided, just leave it as the default
			localizedTime = time.Now().In(location)
		}
	}

	entryString := fmt.Sprintf("%s %s %s", localizedTime.Format(TimeFormat), logLevel, message)
	logger := sugaredLogger.WithOptions(zap.AddCallerSkip(3))
	switch logLevel {
	case FATAL:
		logger.Fatal(message)
	case CRITICAL:
		logger.Named(internalCriticalLoggerName).Error(message)
	case ERROR:
		logger.Error(message)
	case WARNING:
		logger.Warn(message)
	case NOTICE:
		logger.Named(internalNoticeLoggerName).Info(message)
	case INFO:
		logger.Info(message)
	case DEBUG:
		logger.Debug(message)
	}
	return entryString
}

func writeSyslog(logLevel LogLevel, message string) error {
	syslogMutex.RLock()
	defer syslogMutex.RUnlock()
	if syslogWriter == nil || logLevel > LogLevel(syslogLevel.Load()) {
		return nil
	}
	switch logLevel {
	case FATAL:
		return syslogWriter.Emerg(message)
	case CRITICAL:
		return syslogWriter.Crit(message)
	case ERROR:
		return syslogWriter.Err(message)
	case WARNING:
		return syslogWriter.Warning(message)
	case NOTICE:
		return syslogWriter.Notice(message)
	case INFO:
		return syslogWriter.Info(message)
	case DEBUG:
		return syslogWriter.Debug(message)
	default:
		return nil
	}
}

type stderrWriteSyncer struct{}

func (stderrWriteSyncer) Write(data []byte) (int, error) {
	return os.Stderr.Write(data)
}

func (stderrWriteSyncer) Sync() error {
	return nil
}

func newSugaredLogger() *zap.SugaredLogger {
	consoleCore := zapcore.NewCore(newConsoleEncoder(), stderrWriteSyncer{}, zapLevel)
	core := zapcore.NewTee(consoleCore, &syslogCore{encoder: newSyslogEncoder()})
	return zap.New(
		core,
		zap.AddCaller(),
		zap.ErrorOutput(stderrWriteSyncer{}),
		zap.WithFatalHook(closeOnFatalHook{}),
	).Sugar()
}

type closeOnFatalHook struct{}

func (closeOnFatalHook) OnWrite(*zapcore.CheckedEntry, []zapcore.Field) {
	if err := Close(); err != nil {
		fmt.Fprintf(os.Stderr, "logger close failed: %v\n", err)
	}
	os.Exit(1)
}

func newConsoleEncoder() zapcore.Encoder {
	return zapcore.NewConsoleEncoder(zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		CallerKey:      "caller",
		MessageKey:     "msg",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    encodeLevel,
		EncodeTime:     zapcore.TimeEncoderOfLayout(TimeFormat),
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   encodeCaller,
	})
}

func newSyslogEncoder() zapcore.Encoder {
	return zapcore.NewConsoleEncoder(zapcore.EncoderConfig{
		CallerKey:    "caller",
		MessageKey:   "msg",
		LineEnding:   zapcore.DefaultLineEnding,
		EncodeCaller: encodeCaller,
	})
}

type syslogCore struct {
	encoder zapcore.Encoder
	fields  []zapcore.Field
}

func (core *syslogCore) Enabled(level zapcore.Level) bool {
	syslogMutex.RLock()
	enabled := syslogWriter != nil
	syslogMutex.RUnlock()
	if !enabled || level < zapLevel.Level() {
		return false
	}
	return level >= toZapLevel(LogLevel(syslogLevel.Load()))
}

func (core *syslogCore) With(fields []zapcore.Field) zapcore.Core {
	clonedFields := make([]zapcore.Field, 0, len(core.fields)+len(fields))
	clonedFields = append(clonedFields, core.fields...)
	clonedFields = append(clonedFields, fields...)
	return &syslogCore{
		encoder: core.encoder.Clone(),
		fields:  clonedFields,
	}
}

func (core *syslogCore) Check(entry zapcore.Entry, checkedEntry *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if core.Enabled(entry.Level) {
		return checkedEntry.AddCore(entry, core)
	}
	return checkedEntry
}

func (core *syslogCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	logLevel := fromZapEntry(entry)
	if logLevel > LogLevel(syslogLevel.Load()) {
		return nil
	}
	allFields := make([]zapcore.Field, 0, len(core.fields)+len(fields))
	allFields = append(allFields, core.fields...)
	allFields = append(allFields, fields...)
	buffer, err := core.encoder.Clone().EncodeEntry(entry, allFields)
	if err != nil {
		return fmt.Errorf("encode syslog entry: %w", err)
	}
	defer buffer.Free()
	message := strings.TrimSuffix(buffer.String(), zapcore.DefaultLineEnding)
	if err := writeSyslog(logLevel, message); err != nil {
		return fmt.Errorf("syslog write failed: %w", err)
	}
	return nil
}

func (core *syslogCore) Sync() error {
	return nil
}

func fromZapEntry(entry zapcore.Entry) LogLevel {
	switch entry.LoggerName {
	case internalNoticeLoggerName:
		return NOTICE
	case internalCriticalLoggerName:
		return CRITICAL
	}
	switch entry.Level {
	case zapcore.FatalLevel:
		return FATAL
	case zapcore.PanicLevel, zapcore.DPanicLevel:
		return CRITICAL
	case zapcore.ErrorLevel:
		return ERROR
	case zapcore.WarnLevel:
		return WARNING
	case zapcore.InfoLevel:
		return INFO
	default:
		return DEBUG
	}
}

// Sugar returns the process-wide SugaredLogger for structured logging.
func Sugar() *zap.SugaredLogger {
	return sugaredLogger
}

// Sync flushes buffered log entries.
func Sync() error {
	return sugaredLogger.Sync()
}

// RegisterCloseHook registers another process-owned logging sink for Close.
func RegisterCloseHook(hook func() error) {
	if hook == nil {
		return
	}
	closeHooksMutex.Lock()
	closeHooks = append(closeHooks, hook)
	closeHooksMutex.Unlock()
}

// Close flushes logs and closes the optional syslog sink.
func Close() error {
	hooksErr := runCloseHooks()
	syslogMutex.Lock()
	writer := syslogWriter
	syslogWriter = nil
	syslogMutex.Unlock()
	if writer == nil {
		return errors.Join(hooksErr, Sync())
	}
	return errors.Join(hooksErr, Sync(), writer.Close())
}

func runCloseHooks() error {
	closeHooksMutex.Lock()
	hooks := closeHooks
	closeHooks = nil
	closeHooksMutex.Unlock()
	errs := make([]error, 0, len(hooks))
	for _, hook := range hooks {
		errs = append(errs, hook())
	}
	return errors.Join(errs...)
}

func encodeLevel(level zapcore.Level, encoder zapcore.PrimitiveArrayEncoder) {
	encoder.AppendString("[" + level.CapitalString() + "]")
}

func encodeCaller(caller zapcore.EntryCaller, encoder zapcore.PrimitiveArrayEncoder) {
	if !caller.Defined {
		encoder.AppendString("[undefined]")
		return
	}
	encoder.AppendString("[" + caller.TrimmedPath() + "]")
}

func toZapLevel(level LogLevel) zapcore.Level {
	switch level {
	case FATAL:
		return zap.FatalLevel
	case CRITICAL, ERROR:
		return zap.ErrorLevel
	case WARNING:
		return zap.WarnLevel
	case NOTICE, INFO:
		return zap.InfoLevel
	case DEBUG:
		return zap.DebugLevel
	default:
		return zap.DebugLevel
	}
}

// logEntry emits a formatted log entry
func logEntry(logLevel LogLevel, message string, args ...interface{}) string {
	entryString := message
	for _, s := range args {
		entryString += fmt.Sprintf(" %s", s)
	}
	return emit(logLevel, entryString)
}

// logErrorEntry emits a log entry based on given error object
func logErrorEntry(logLevel LogLevel, err error) error {
	if err == nil {
		// No error
		return nil
	}
	entryString := fmt.Sprintf("%+v", err)
	emit(logLevel, entryString)
	if printStackTrace.Load() {
		debug.PrintStack()
	}
	return err
}

func Debug(message string, args ...interface{}) string {
	return logEntry(DEBUG, message, args...)
}

func Debugf(message string, args ...interface{}) string {
	return logFormattedEntry(DEBUG, message, args...)
}

func Info(message string, args ...interface{}) string {
	return logEntry(INFO, message, args...)
}

func Infof(message string, args ...interface{}) string {
	return logFormattedEntry(INFO, message, args...)
}

func Notice(message string, args ...interface{}) string {
	return logEntry(NOTICE, message, args...)
}

func Noticef(message string, args ...interface{}) string {
	return logFormattedEntry(NOTICE, message, args...)
}

func Warning(message string, args ...interface{}) error {
	return errors.New(logEntry(WARNING, message, args...))
}

func Warningf(message string, args ...interface{}) error {
	return errors.New(logFormattedEntry(WARNING, message, args...))
}

func Error(message string, args ...interface{}) error {
	return errors.New(logEntry(ERROR, message, args...))
}

func Errorf(message string, args ...interface{}) error {
	return errors.New(logFormattedEntry(ERROR, message, args...))
}

func Errore(err error) error {
	return logErrorEntry(ERROR, err)
}

func Critical(message string, args ...interface{}) error {
	return errors.New(logEntry(CRITICAL, message, args...))
}

func Criticalf(message string, args ...interface{}) error {
	return errors.New(logFormattedEntry(CRITICAL, message, args...))
}

func Criticale(err error) error {
	return logErrorEntry(CRITICAL, err)
}

// Fatal emits a FATAL level entry and exists the program
func Fatal(message string, args ...interface{}) error {
	logEntry(FATAL, message, args...)
	os.Exit(1)
	return errors.New(logEntry(CRITICAL, message, args...))
}

// Fatalf emits a FATAL level entry and exists the program
func Fatalf(message string, args ...interface{}) error {
	logFormattedEntry(FATAL, message, args...)
	os.Exit(1)
	return errors.New(logFormattedEntry(CRITICAL, message, args...))
}

// Fatale emits a FATAL level entry and exists the program
func Fatale(err error) error {
	logErrorEntry(FATAL, err)
	os.Exit(1)
	return err
}
