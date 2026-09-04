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

package attributes

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/openark/golib/log"
	"github.com/openark/orchestrator/go/db"
)

type hostAttributesRow struct {
	Hostname        string `gorm:"column:hostname"`
	AttributeName   string `gorm:"column:attribute_name"`
	AttributeValue  string `gorm:"column:attribute_value"`
	SubmitTimestamp string `gorm:"column:submit_timestamp"`
	ExpireTimestamp string `gorm:"column:expire_timestamp"`
}

func readResponse(res *http.Response, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}

	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	if res.Status == "500" {
		return body, errors.New("Response Status 500")
	}

	return body, nil
}

// SetHostAttributes
func SetHostAttributes(hostname string, attributeName string, attributeValue string) error {
	_, err := db.ExecOrchestrator(`
			replace
				into host_attributes (
					hostname, attribute_name, attribute_value, submit_timestamp, expire_timestamp
				) VALUES (
					?, ?, ?, NOW(), NULL
				)
			`,
		hostname,
		attributeName,
		attributeValue,
	)
	if err != nil {
		return log.Errore(err)
	}

	return err
}

func getHostAttributesByClause(whereClause string, args []interface{}) ([]HostAttributes, error) {
	res := []HostAttributes{}
	query := fmt.Sprintf(`
		select
			hostname,
			attribute_name,
			attribute_value,
			submit_timestamp ,
			ifnull(expire_timestamp, '') as expire_timestamp
		from
			host_attributes
		%s
		order by
			hostname, attribute_name
		`, whereClause)

	rows, err := db.QueryOrchestratorRows[hostAttributesRow](context.Background(), query, args...)
	for _, row := range rows {
		res = append(res, HostAttributes{
			Hostname:        row.Hostname,
			AttributeName:   row.AttributeName,
			AttributeValue:  row.AttributeValue,
			SubmitTimestamp: row.SubmitTimestamp,
			ExpireTimestamp: row.ExpireTimestamp,
		})
	}

	if err != nil {
		log.Errore(err)
	}
	return res, err
}

// GetHostAttributesByMatch
func GetHostAttributesByMatch(hostnameMatch string, attributeNameMatch string, attributeValueMatch string) ([]HostAttributes, error) {
	terms := []string{}
	args := []interface{}{}
	if hostnameMatch != "" {
		terms = append(terms, ` hostname rlike ? `)
		args = append(args, hostnameMatch)
	}
	if attributeNameMatch != "" {
		terms = append(terms, ` attribute_name rlike ? `)
		args = append(args, attributeNameMatch)
	}
	if attributeValueMatch != "" {
		terms = append(terms, ` attribute_value rlike ? `)
		args = append(args, attributeValueMatch)
	}

	if len(terms) == 0 {
		return getHostAttributesByClause("", args)
	}
	whereCondition := fmt.Sprintf(" where %s ", strings.Join(terms, " and "))

	return getHostAttributesByClause(whereCondition, args)
}

// GetHostAttribute expects to return a single attribute for a given hostname/attribute-name combination
// or error on empty result
func GetHostAttribute(hostname string, attributeName string) (string, error) {
	whereClause := `where hostname=? and attribute_name=?`
	attributes, err := getHostAttributesByClause(whereClause, []interface{}{hostname, attributeName})
	if err != nil {
		return "", err
	}
	return hostAttributeValue(attributes, hostname, attributeName)
}

func hostAttributeValue(attributes []HostAttributes, hostname string, attributeName string) (string, error) {
	if len(attributes) == 0 {
		return "", log.Errorf("No attribute found for %+v, %+v", hostname, attributeName)
	}
	return attributes[0].AttributeValue, nil
}

// SetGeneralAttribute sets an attribute not associated with a host. Its a key-value thing
func SetGeneralAttribute(attributeName string, attributeValue string) error {
	if attributeName == "" {
		return nil
	}
	return SetHostAttributes("*", attributeName, attributeValue)
}

// GetGeneralAttribute expects to return a single attribute value (not associated with a specific hostname)
func GetGeneralAttribute(attributeName string) (result string, err error) {
	return GetHostAttribute("*", attributeName)
}

// GetHostAttributesByAttribute
func GetHostAttributesByAttribute(attributeName string, valueMatch string) ([]HostAttributes, error) {
	if valueMatch == "" {
		valueMatch = ".?"
	}
	whereClause := ` where attribute_name = ? and attribute_value rlike ?`

	return getHostAttributesByClause(whereClause, []interface{}{attributeName, valueMatch})
}
