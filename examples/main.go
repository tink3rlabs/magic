package main

import (
	"embed"
	"fmt"

	"github.com/google/uuid"
	"github.com/tink3rlabs/magic/storage"
)

type Item struct {
	Tenant      string `json:"tenant"`
	Id          string `json:"id"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

//go:embed config
var configFS embed.FS

func main() {
	storage.ConfigFs = configFS

	config := map[string]string{}

	// config = map[string]string{
	// 	"provider": "postgresql",
	// 	"host":     "host.docker.internal",
	// 	"port":     "5432",
	// 	"user":     "todo",
	// 	"password": "todo",
	// 	"dbname":   "todo",
	// 	"schema":   "todo",
	// }

	// config = map[string]string{
	// 	"provider":   "dynamodb",
	// 	"region":     "us-west-2",
	// 	"endpoint":   "http://host.docker.internal:8000",
	// 	"access_key": "fake",
	// 	"secret_key": "fake",
	// 	"global":     "false",
	// }

	// config := map[string]string{
	// 	"provider":          "cosmosdb",
	// 	"endpoint":          "https://your-cosmosdb-account.documents.azure.com:443/",
	// 	"key":               "your-cosmosdb-key",
	// 	"database":          "magic",
	// }

	s, err := storage.StorageAdapterFactory{}.GetInstance(storage.MEMORY, config)
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println(s.Ping())

	storage.NewDatabaseMigration(s).Migrate()

	id, err := uuid.NewV7()
	if err != nil {
		fmt.Println(err)
	}

	fmt.Println(id)

	it := Item{
		Tenant:      "foo.io",
		Id:          id.String(),
		Kind:        "foo-kind",
		Name:        "bar",
		Description: "foo bar",
	}

	fmt.Println("creating item")
	err = s.Create(it)
	if err != nil {
		fmt.Println(err)
	}

	fmt.Println("getting item")
	var i Item
	err = s.Get(&i, map[string]any{"tenant": it.Tenant, "id": it.Id})
	if err != nil {
		fmt.Println(err)
	}

	fmt.Println(i.Tenant, i.Id, i.Kind, i.Name)

	fmt.Println("updating item")
	i.Name = "updated name"
	err = s.Update(&i, map[string]any{"tenant": it.Tenant, "id": it.Id})
	if err != nil {
		fmt.Println(err)
	}

	fmt.Println(i.Tenant, i.Id, i.Kind, i.Name)

	fmt.Println("deleting item")
	err = s.Delete(&i, map[string]any{"tenant": it.Tenant, "id": it.Id})
	if err != nil {
		fmt.Println(err)
	}

	fmt.Println("getting item")
	var i2 Item
	err = s.Get(&i2, map[string]any{"tenant": it.Tenant, "id": it.Id})
	if err != nil {
		fmt.Println(err)
	}

	fmt.Println(i2.Tenant, i2.Id, i2.Kind, i2.Name)
	list(s)
}

func list(s storage.StorageAdapter) {
	fmt.Println("creating 5 new items")
	fmt.Println("listing items")

	for i := 0; i < 10; i++ {
		id, err := uuid.NewV7()
		if err != nil {
			fmt.Println(err)
		}

		var tenant string
		if i%2 == 0 {
			tenant = "even"
		} else {
			tenant = "odd"
		}

		it := Item{
			Id:          id.String(),
			Tenant:      tenant,
			Kind:        "foo-kind",
			Name:        fmt.Sprintf("item %v", i),
			Description: fmt.Sprintf("Description for item %v", i),
		}

		err = s.Create(it)
		if err != nil {
			fmt.Println(err)
		}
	}

	var items []Item
	iterator := 0
	next := ""

	for {
		var i []Item
		var err error
		next, err = s.List(&i, "Id", map[string]any{"tenant": "even"}, 3, next)
		if err != nil {
			fmt.Println(err)
			break
		}

		items = append(items, i...)
		if next == "" {
			break
		}
		iterator++
	}

	for _, i := range items {
		fmt.Println(i.Tenant, i.Id, i.Kind, i.Name)
	}
}
