package expander

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"reflect"
	"strings"
)

type MongoDbRefResolver struct {
	uris             map[string]string
	makeBulkRequests bool
}

func NewMongoDbRefResolver(uriMap map[string]string, makeBulkRequests bool) MongoDbRefResolver {
	return MongoDbRefResolver{uris: uriMap, makeBulkRequests: makeBulkRequests}
}

type MongoDBRef struct {
	Id         string `json:"_id"`
	Collection string `json:"collection"`
	Database   string `json:"database"`
}

func (this MongoDbRefResolver) IsReference(t reflect.Value) (Reference, bool) {
	var mongoRef MongoDBRef
	var reference Reference

	if t.Kind() != reflect.Struct {
		return reference, false
	}

	if t.NumField() != 3 {
		return reference, false
	}

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		ft := t.Type().Field(i)

		if ft.Name == "Collection" {
			mongoRef.Collection = f.String()
		} else if ft.Name == "Database" {
			mongoRef.Database = f.String()
		} else if ft.Name == "Id" {
			objectId := fmt.Sprintf("%v", f.Interface())
			if strings.HasPrefix(objectId, "ObjectId(") {
				objectId = strings.Replace(objectId, "ObjectId(", "", -1)
				objectId = strings.Replace(objectId, ")", "", -1)
			}
			mongoRef.Id = objectId
		}
	}
	if mongoRef.Collection == "" || mongoRef.Id == "" {
		return reference, false
	}

	reference.OriginalReference = mongoRef
	reference.Id = mongoRef.Id
	return reference, true
}

func (this MongoDbRefResolver) GetName() string {
	return "MongoDbRefResolver"
}

func (this MongoDbRefResolver) ResolveRef(refs []Reference) map[string]interface{} {
	if this.makeBulkRequests {
		return this.resolveWithBulkRequests(refs)
	} else {
		return this.resolveStupid(refs)
	}

}

var makeGetCall = func(uri *url.URL) ([]byte, bool) {
	if uri == nil {
		return []byte(""), false
	}
	response, err := http.Get(uri.String())
	if err != nil {
		fmt.Println(err)
		return []byte(""), false
	}

	defer response.Body.Close()
	body, err := ioutil.ReadAll(response.Body)

	if err != nil {
		fmt.Println("Error while reading content of response body. It was: ", err)
	}

	return body, true
}

func (this *MongoDbRefResolver) resolveStupid(refs []Reference) map[string]interface{} {
	callResults := make(map[string]interface{})

	for _, ref := range refs {
		collection := ref.OriginalReference.(MongoDBRef).Collection
		id := ref.OriginalReference.(MongoDBRef).Id
		callURL := this.uris[collection] + id
		url, _ := url.ParseRequestURI(callURL)

		responseBytes, ok := makeGetCall(url)
		if ok {
			var response map[string]interface{}
			_ = json.Unmarshal(responseBytes, &response)
			callResults[id] = response
		}

	}
	return callResults
}

func (this *MongoDbRefResolver) resolveWithBulkRequests(refs []Reference) map[string]interface{} {
	perCollectionIds := make(map[string]string)
	for _, task := range refs {
		mongoRef := task.OriginalReference.(MongoDBRef)
		perCollectionIds[mongoRef.Collection] += task.Id + ","
	}

	callResults := make(map[string]interface{})
	for collection, idList := range perCollectionIds {

		callURL := this.uris[collection] + idList
		url, _ := url.ParseRequestURI(callURL)
		responseBytes, ok := makeGetCall(url)
		if ok {
			var response BulkResponseMongoObject
			var responseData BulkResponseData

			_ = json.Unmarshal(responseBytes, &response)
			_ = json.Unmarshal(responseBytes, &responseData)

			for index, mongoObject := range response.Data {
				callResults[mongoObject.Id] = responseData.Data[index]
			}
		}
	}
	return callResults
}
