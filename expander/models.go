package expander

import "reflect"

type Configuration struct {
	Resolvers []Resolver
}

type CacheEntry struct {
	Timestamp int64
	Data      string
}

type DBRef struct {
	Collection string
	Id         interface{}
	Database   string
}

type ObjectId interface {
	Hex() string
}

type Filter struct {
	Children Filters
	Value    string
}

type Filters []Filter

func (m Filters) Contains(v string) bool {
	for _, m := range m {
		if v == m.Value {
			return true
		}
	}

	return false
}

func (m Filters) IsEmpty() bool {
	return len(m) == 0
}

func (m Filters) Get(v string) Filter {
	var result Filter

	if m.IsEmpty() {
		return result
	}

	for _, m := range m {
		if v == m.Value {
			return m
		}
	}

	return result
}

type ExpansionTask struct {
	Resolver  string
	Reference Reference
	Success   func(value interface{})
	Error     func()
}

type Reference struct {
	Id                string
	OriginalReference interface{}
}

type Resolver interface {
	IsReference(reflect.Value) (Reference, bool)
	ResolveRef([]Reference) map[string]interface{}
	GetName() string
}

type WalkStateHolder struct {
	resolveTasks *[]ExpansionTask
}

func (this *WalkStateHolder) GetExpansionTasks() []ExpansionTask {
	return *this.resolveTasks
}

func (this *WalkStateHolder) AddExpansionTask(resolveTask ExpansionTask) {
	realArray := *this.resolveTasks
	result := append(realArray, resolveTask)
	*this.resolveTasks = result
}

func UniqueKey(collection string, id string) string {
	return collection + "." + id
}

type MongoObject struct {
	Id string `json:"_id"`
}

type BulkResponseMongoObject struct {
	Data []MongoObject `json:"data"`
}

type BulkResponseData struct {
	Data []map[string]interface{} `json:"data"`
}
