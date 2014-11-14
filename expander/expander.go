package expander

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// TODO:
// 1. add filters to expansiontask in order to correctly resolve children
// 2. fix other TODOs
const (
	COLLECTION_KEY = "Collection"
	emptyTimeValue = "0001-01-01T00:00:00Z"
)

var resolvers []Resolver

func AddResolver(newResolver Resolver) {
	resolvers = append(resolvers, newResolver)
}

func ClearResolvers() {
	resolvers = []Resolver{}
}

func resolveFilters(expansion, fields string) (expansionFilter Filters, fieldFilter Filters, recursiveExpansion bool, err error) {
	if !validateFilterFormat(expansion) {
		err = errors.New("expansionFilter for filtering was not correct")
		return
	}
	if !validateFilterFormat(fields) {
		err = errors.New("fieldFilter for filtering was not correct")
		return
	}

	fieldFilter, _ = buildFilterTree(fields)

	if expansion != "*" {
		expansionFilter, _ = buildFilterTree(expansion)
	} else if fields != "*" && fields != "" {
		expansionFilter, _ = buildFilterTree(fields)
	} else {
		recursiveExpansion = true
	}
	return
}

//TODO: TagFields & BSONFields
func Expand(data interface{}, expansion, fields string) map[string]interface{} {

	expansionFilter, fieldFilter, recursiveExpansion, err := resolveFilters(expansion, fields)
	if err != nil {
		expansionFilter = Filters{}
		fieldFilter = Filters{}
		fmt.Printf("Warning: Filter was not correct, expansionFilter: '%v' fieldFilter: '%v', error: %v \n", expansion, fields, err)
	}

	resolveTasks := []ExpansionTask{}
	walkStateHolder := WalkStateHolder{&resolveTasks}
	expanded := walkByExpansion(data, walkStateHolder, expansionFilter, recursiveExpansion)
	executeExpansionTasks(walkStateHolder.GetExpansionTasks(), recursiveExpansion)

	filtered := walkByFilter(expanded, fieldFilter)

	return filtered
}

func ExpandArray(data interface{}, expansion, fields string) []interface{} {
	expansionFilter, fieldFilter, recursiveExpansion, err := resolveFilters(expansion, fields)
	if err != nil {
		expansionFilter = Filters{}
		fieldFilter = Filters{}
		fmt.Printf("Warning: Filter was not correct, expansionFilter: '%v' fieldFilter: '%v', error: %v \n", expansionFilter, fieldFilter, err)
	}

	var result []interface{}

	if data == nil {
		return result
	}

	v := reflect.ValueOf(data)
	switch data.(type) {
	case reflect.Value:
		v = data.(reflect.Value)
	}

	if v.Kind() != reflect.Slice {
		return result
	}

	v = v.Slice(0, v.Len())
	for i := 0; i < v.Len(); i++ {
		resolveTasks := []ExpansionTask{}
		walkStateHolder := WalkStateHolder{&resolveTasks}
		arrayItem := walkByExpansion(v.Index(i), walkStateHolder, expansionFilter, recursiveExpansion)
		executeExpansionTasks(walkStateHolder.GetExpansionTasks(), recursiveExpansion)
		arrayItem = walkByFilter(arrayItem, fieldFilter)
		result = append(result, arrayItem)
	}
	return result
}

func executeExpansionTasks(expansionTasks []ExpansionTask, recursive bool) {
	tasksByResolver := make(map[string][]ExpansionTask)
	for _, task := range expansionTasks {
		tasksByResolver[task.Resolver] = expansionTasks
	}

	for _, resolver := range resolvers {
		tasks := tasksByResolver[resolver.GetName()]
		var refs []Reference
		for _, task := range tasks {
			refs = append(refs, task.Reference)
		}
		result := resolver.ResolveRef(refs)
		for _, task := range tasks {
			if value, ok := result[task.Reference.Id]; ok {
				task.Success(value)
			} else if task.Error != nil {
				task.Error()
			}
		}

	}
}

func walkByFilter(data map[string]interface{}, filters Filters) map[string]interface{} {
	result := make(map[string]interface{})

	if data == nil {
		return result
	}

	for k, v := range data {
		if filters.IsEmpty() || filters.Contains(k) {
			ft := reflect.ValueOf(v)

			result[k] = v
			subFilters := filters.Get(k).Children

			if v == nil {
				continue
			}

			switch ft.Type().Kind() {
			case reflect.Map:
				result[k] = walkByFilter(v.(map[string]interface{}), subFilters)
			case reflect.Slice:
				if ft.Len() == 0 {
					continue
				}

				switch ft.Index(0).Kind() {
				case reflect.Map:
					children := make([]map[string]interface{}, 0)
					for _, child := range v.([]map[string]interface{}) {
						item := walkByFilter(child, subFilters)
						children = append(children, item)
					}
					result[k] = children
				default:
					children := make([]interface{}, 0)
					for _, child := range v.([]interface{}) {
						cft := reflect.TypeOf(child)

						if cft.Kind() == reflect.Map {
							item := walkByFilter(child.(map[string]interface{}), subFilters)
							children = append(children, item)
						} else {
							children = append(children, child)
						}
					}
					result[k] = children
				}
			}
		}
	}

	return result
}

func walkByExpansion(data interface{}, walkStateHolder WalkStateHolder, filters Filters, recursive bool) map[string]interface{} {
	result := make(map[string]interface{})

	if data == nil {
		return result
	}

	v := reflect.ValueOf(data)
	switch data.(type) {
	case reflect.Value:
		v = data.(reflect.Value)
	}
	if v.Type().Kind() == reflect.Ptr {
		v = v.Elem()
	}

	//	var resultWriteMutex = sync.Mutex{}
	var writeToResult = func(key string, value interface{}, omitempty bool) {
		if omitempty && isEmptyValue(reflect.ValueOf(value)) {
			delete(result, key)
		} else {
			result[key] = value
		}
	}

	// check if root is db ref
	reference, resolver, ok := testForReferences(v)
	if ok && recursive {
		placeholder := make(map[string]interface{})

		var resolveTask ExpansionTask
		resolveTask.Reference = reference
		resolveTask.Resolver = resolver.GetName()
		resolveTask.Success = func(value interface{}) {
			valueAsMap := value.(map[string]interface{})
			for k, v := range valueAsMap {
				placeholder[k] = v
			}
		}
		walkStateHolder.AddExpansionTask(resolveTask)
		return placeholder
	}

	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		ft := v.Type().Field(i)

		if f.Kind() == reflect.Ptr {
			f = f.Elem()
		}
		var omitempty = false

		key := ft.Name
		tag := ft.Tag.Get("json")
		if tag != "" {
			tags := strings.Split(tag, ",")
			key = tags[0]
			for _, currentPart := range tags {
				if currentPart == "omitempty" {
					omitempty = true
				}
			}
		}

		options := func() (bool, string) {
			return recursive, key
		}

		reference, resolver, ok := testForReferences(f)
		if ok {
			if filters.Contains(key) || recursive {

				var resolveTask ExpansionTask
				resolveTask.Reference = reference
				resolveTask.Resolver = resolver.GetName()
				resolveTask.Success = func(value interface{}) {
					writeToResult(key, value, omitempty)
				}
				resolveTask.Error = func() {
					writeToResult(key, f.Interface(), omitempty)
				}
				walkStateHolder.AddExpansionTask(resolveTask)

			} else {
				writeToResult(key, f.Interface(), omitempty)
			}
		} else {
			val := getValue(f, walkStateHolder, filters, options)
			writeToResult(key, val, omitempty)
			switch val.(type) {
			case string:
				unquoted, err := strconv.Unquote(val.(string))
				if err == nil {
					writeToResult(key, unquoted, omitempty)
				}
			}
		}

	}

	return result
}

func testForReferences(value reflect.Value) (Reference, Resolver, bool) {
	var ref Reference
	for _, resolver := range resolvers {
		if ref, ok := resolver.IsReference(value); ok {
			return ref, resolver, true
		}
	}
	return ref, nil, false
}

func getValue(t reflect.Value, walkStateHolder WalkStateHolder, filters Filters, options func() (bool, string)) interface{} {
	recursive, parentKey := options()

	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return t.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return t.Uint()
	case reflect.Float32, reflect.Float64:
		return t.Float()
	case reflect.Bool:
		return t.Bool()
	case reflect.String:
		return t.String()
	case reflect.Slice:
		var result = []interface{}{}

		for i := 0; i < t.Len(); i++ {
			current := t.Index(i)

			if filters.Contains(parentKey) || recursive {

				reference, resolver, ok := testForReferences(current)
				if ok {
					result = append(result, current.Interface())

					var resolveTask ExpansionTask
					var localCounter = i
					resolveTask.Reference = reference
					resolveTask.Resolver = resolver.GetName()
					resolveTask.Success = func(resolvedValue interface{}) {
						result[localCounter] = resolvedValue
					}
					walkStateHolder.AddExpansionTask(resolveTask)

				} else {
					result = append(result, getValue(current, walkStateHolder, filters.Get(parentKey).Children, options))
				}
			} else {
				result = append(result, getValue(current, walkStateHolder, filters.Get(parentKey).Children, options))
			}
		}

		return result
	case reflect.Map:
		result := make(map[string]interface{})

		for _, v := range t.MapKeys() {
			key := v.Interface().(string)
			result[key] = getValue(t.MapIndex(v), walkStateHolder, filters.Get(key).Children, options)
		}

		return result
	case reflect.Struct:
		val, ok := t.Interface().(json.Marshaler)
		if ok {
			bytes, err := val.(json.Marshaler).MarshalJSON()
			if err != nil {
				fmt.Println(err)
			}

			return string(bytes)
		}

		return walkByExpansion(t, walkStateHolder, filters, recursive)
	default:
		return t.Interface()
	}

	return ""
}

func expandChildren(m map[string]interface{}, filters Filters, recursive bool) map[string]interface{} {
	result := make(map[string]interface{})
	if true {
		return result
	}
	for key, v := range m {
		ft := reflect.TypeOf(v)
		result[key] = v
		if v == nil {
			continue
		}
		if ft.Kind() == reflect.Map && (recursive || filters.Contains(key)) {
			child := v.(map[string]interface{})
			_, _ = child["ref"]

			/*if found {
				resource, ok := getResourceFrom(uri.(string), filters, recursive)
				if ok {
					result[key] = resource
				}
			}*/
		}
	}

	return result
}

func validateFilterFormat(filter string) bool {
	runes := []rune(filter)

	var bracketCounter = 0

	for i := range runes {
		if runes[i] == '(' {
			bracketCounter++
		} else if runes[i] == ')' {
			bracketCounter--
			if bracketCounter < 0 {
				return false
			}
		}
	}
	return bracketCounter == 0

}

func buildFilterTree(statement string) ([]Filter, int) {
	var result []Filter
	const comma uint8 = ','
	const openBracket uint8 = '('
	const closeBracket uint8 = ')'

	if statement == "*" {
		return result, -1
	}

	statement = strings.Replace(statement, " ", "", -1)
	if len(statement) == 0 {
		return result, -1
	}

	indexAfterSeparation := 0
	closeIndex := 0

	for i := 0; i < len(statement); i++ {
		switch statement[i] {
		case openBracket:
			filter := Filter{Value: string(statement[indexAfterSeparation:i])}
			filter.Children, closeIndex = buildFilterTree(statement[i+1:])
			result = append(result, filter)
			i = i + closeIndex
			indexAfterSeparation = i + 1
			closeIndex = indexAfterSeparation
		case comma:
			filter := Filter{Value: string(statement[indexAfterSeparation:i])}
			if filter.Value != "" {
				result = append(result, filter)
			}
			indexAfterSeparation = i + 1
		case closeBracket:
			filter := Filter{Value: string(statement[indexAfterSeparation:i])}
			if filter.Value != "" {
				result = append(result, filter)
			}

			return result, i + 1
		}
	}

	if indexAfterSeparation > closeIndex {
		result = append(result, Filter{Value: string(statement[indexAfterSeparation:])})
	}

	if indexAfterSeparation == 0 {
		result = append(result, Filter{Value: statement})
	}

	return result, -1
}

// this function is a modification from isEmptyValue in json/encode.go
func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice:
		return v.Len() == 0
	case reflect.String:
		return v.Len() == 0 || v.Interface() == emptyTimeValue
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Ptr:
		return v.IsNil()
	}
	return false
}
