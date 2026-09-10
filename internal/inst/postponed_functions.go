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
	"sync"

	"github.com/openark/orchestrator/internal/golib/log"
)

type PostponedFunctionsContainer struct {
	waitGroup    sync.WaitGroup
	mutex        sync.Mutex
	descriptions []string
}

func NewPostponedFunctionsContainer() *PostponedFunctionsContainer {
	postponedFunctionsContainer := &PostponedFunctionsContainer{
		descriptions: []string{},
	}
	return postponedFunctionsContainer
}

func (container *PostponedFunctionsContainer) AddPostponedFunction(postponedFunction func() error, description string) {
	container.mutex.Lock()
	defer container.mutex.Unlock()

	container.descriptions = append(container.descriptions, description)

	container.waitGroup.Go(func() {
		postponedFunction()
	})
}

func (container *PostponedFunctionsContainer) Wait() {
	log.Debugf("PostponedFunctionsContainer: waiting on %+v postponed functions", container.Len())
	container.waitGroup.Wait()
	log.Debugf("PostponedFunctionsContainer: done waiting")
}

func (container *PostponedFunctionsContainer) Len() int {
	container.mutex.Lock()
	defer container.mutex.Unlock()

	return len(container.descriptions)
}

func (container *PostponedFunctionsContainer) Descriptions() []string {
	container.mutex.Lock()
	defer container.mutex.Unlock()

	return container.descriptions
}
