/*
   Copyright 2015 Shlomi Noach, courtesy Booking.com

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

	"github.com/openark/orchestrator/internal/golib/log"
	"github.com/openark/orchestrator/internal/repository/metadata"
)

func PutInstanceTag(instanceKey *InstanceKey, tag *Tag) (err error) {
	return metadata.PutInstanceTag(context.Background(), instanceKey.Hostname, instanceKey.Port, tag.TagName, tag.TagValue)
}

func Untag(instanceKey *InstanceKey, tag *Tag) (tagged *InstanceKeyMap, err error) {
	if tag == nil {
		return nil, log.Errorf("Untag: tag is nil")
	}
	if tag.Negate {
		return nil, log.Errorf("Untag: does not support negation")
	}
	if instanceKey == nil && !tag.HasValue {
		return nil, log.Errorf("Untag: either indicate an instance or a tag value. Will not delete on-valued tag across instances")
	}
	tagged = NewInstanceKeyMap()
	hostname, port, matchInstance := "", 0, instanceKey != nil
	if instanceKey != nil {
		hostname, port = instanceKey.Hostname, instanceKey.Port
	}
	rows, err := metadata.DeleteInstanceTags(
		context.Background(), hostname, port, matchInstance, tag.TagName, tag.TagValue, tag.HasValue,
	)
	for _, row := range rows {
		key, _ := NewResolveInstanceKey(row.Hostname, row.Port)
		tagged.AddKey(*key)
	}
	if err != nil {
		return tagged, log.Errore(err)
	}
	AuditOperation("delete-instance-tag", instanceKey, tag.String())
	return tagged, nil
}

func ReadInstanceTag(instanceKey *InstanceKey, tag *Tag) (tagExists bool, err error) {
	rows, err := metadata.ReadInstanceTagValue(context.Background(), instanceKey.Hostname, instanceKey.Port, tag.TagName)
	if err == nil && len(rows) > 0 {
		tag.TagValue = rows[0]
		tagExists = true
	}

	return tagExists, log.Errore(err)
}

func InstanceTagExists(instanceKey *InstanceKey, tag *Tag) (tagExists bool, err error) {
	return ReadInstanceTag(instanceKey, &Tag{TagName: tag.TagName})
}

func ReadInstanceTags(instanceKey *InstanceKey) (tags [](*Tag), err error) {
	tags = [](*Tag){}
	rows, err := metadata.ReadInstanceTags(context.Background(), instanceKey.Hostname, instanceKey.Port)
	for _, row := range rows {
		tag := &Tag{
			TagName:  row.Name,
			TagValue: row.Value,
		}
		tags = append(tags, tag)
	}

	return tags, log.Errore(err)
}

func GetInstanceKeysByTag(tag *Tag) (tagged *InstanceKeyMap, err error) {
	if tag == nil {
		return nil, log.Errorf("GetInstanceKeysByTag: tag is nil")
	}
	tagged = NewInstanceKeyMap()
	rows, err := metadata.ReadInstanceKeysByTag(context.Background(), tag.TagName, tag.TagValue, tag.HasValue, tag.Negate)
	for _, row := range rows {
		key, _ := NewResolveInstanceKey(row.Hostname, row.Port)
		tagged.AddKey(*key)
	}
	return tagged, log.Errore(err)
}
