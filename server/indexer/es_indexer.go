package indexer

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	// "fmt"
	"log"
	"path"
	// "strings"

	"gopkg.in/olivere/elastic.v3"
)

type ESIndexer struct {
	client *elastic.Client
}

type FileIndex struct {
	Blob     string     `json:"blob"`
	Metadata []Metadata `json:"metadata"`
	Content  string     `json:"content"`
}

type Metadata struct {
	Project string `json:"project"`
	Repo    string `json:"repo"`
	Refs    string `json:"refs"`
	Path    string `json:"path"`
	Ext     string `json:"ext"`
}

type Hit struct {
	Source Source `json:"_source"`
	// Highlight map[string][]string `json:"highlight"`
	Highlight []HighlightSource `json:"highlight"`
}
type Source struct {
	Blob     string     `json:"blob"`
	Metadata []Metadata `json:"metadata"`
}
type HighlightSource struct {
	Offset  int    `json:"offset"`
	Content string `json:"content"`
}

var LINE_TAG = regexp.MustCompile(`^\[([0-9]+)\]\s(.*)`)

func NewESIndexer() Indexer {
	client, err := elastic.NewClient(elastic.SetURL())
	if err != nil {
		panic(err)
	}
	i := &ESIndexer{client: client}
	i.Init()
	return i
}

func (esi *ESIndexer) Init() {

	// esi.client.DeleteIndex("gosource").Do()
	_, err := esi.client.CreateIndex("gosource").BodyString(`
{
	settings: {
		analysis: {
			filter: {
				pos_filter: {
					type: "kuromoji_part_of_speech",
					stoptags: ["助詞-格助詞-一般", "助詞-終助詞"]
				},
				greek_lowercase_filter: {
					type: "lowercase",
					language: "greek"
				}
			},
			char_filter: {
				remove_tags: {
					type: "pattern_replace",
					pattern: "^\\[[0-9]+\\]\\u0020",
					flags: "MULTILINE",
					replacement: ""
				}
			},
			analyzer: {
				path_analyzer: {
					type: "custom",
					tokenizer: "path_tokenizer"
				},
				kuromoji_analyzer: {
					type: "custom",
					tokenizer: "kuromoji_tokenizer",
					char_filter: ["remove_tags"],
					filter: ["kuromoji_baseform", "pos_filter", "greek_lowercase_filter", "cjk_width"]
				}
			},
			tokenizer: {
				path_tokenizer: {
					type: "path_hierarchy",
					reverse: true
				}
			}
		}
	},
	mappings: {
		file: {
			properties: {
				blob: {
					type: "string",
					index: "not_analyzed"
				},
				metadata: {
					type: "nested",
					properties: {
						project: {
							type: "multi_field",
							fields: {
								project: {
									type: "string",
									index: "analyzed"
								},
								full: {
									type: "string",
									index: "not_analyzed"
								}
							}
						},
						repository: {
							type: "multi_field",
							fields: {
								repository: {
									type: "string",
									index: "analyzed"
								},
								full: {
									type: "string",
									index: "not_analyzed"
								}
							}
						},
						refs: {
							type: "multi_field",
							fields: {
								refs: {
									type: "string",
									index: "analyzed"
								},
								full: {
									type: "string",
									index: "not_analyzed"
								}
							}
						},
						path: {
							type: "string",
							analyzer: "path_analyzer"
						},
						ext: {
							type: "string",
							index: "not_analyzed"
						}
					}
				},
				contents: {
					type: "string",
					index_options: "offsets",
					analyzer: "kuromoji_analyzer"
				}
			}
		}
	}
}
		`).Do()

	if err != nil {
		log.Println(err)
	}
}

func (esi *ESIndexer) CreateFileIndex(project string, repo string, branch string, filePath string, blob string, content string) error {

	ext := path.Ext(filePath)

	fileIndex := FileIndex{Blob: blob, Metadata: []Metadata{Metadata{Project: project, Repo: repo, Refs: branch, Path: filePath, Ext: ext}}, Content: content}

	_, err := esi.client.Index().
		Index("gosource").
		Type("file").
		Id(blob).
		BodyJson(fileIndex).
		Refresh(true).
		Do()

	if err != nil {
		return err
	}
	return nil
}

func (esi *ESIndexer) UpsertFileIndex(project string, repo string, branch string, filePath string, blob string, content string) error {

	ext := path.Ext(filePath)

	get, err := esi.client.Get().
		Index("gosource").
		Type("file").
		Id(blob).
		Do()

	if err == nil && get.Found {
		var fileIndex FileIndex
		if err := json.Unmarshal(*get.Source, &fileIndex); err != nil {
			return err
		}
		f := func(x Metadata, i int) bool {
			return x.Project == project &&
				x.Repo == repo &&
				x.Refs == branch &&
				x.Path == filePath
		}
		found := find(f, fileIndex.Metadata)
		if found == nil {
			fileIndex.Metadata = append(fileIndex.Metadata, Metadata{Project: project, Repo: repo, Refs: branch, Path: filePath, Ext: ext})
		}

		_, err := esi.client.Update().
			Index("gosource").
			Type("file").
			Id(blob).
			Doc(fileIndex).
			Do()

		if err != nil {
			log.Println("Upsert Doc error", err)
			return err
		}

	} else {
		lines := strings.Split(content, "\n")
		newLines := []string{}
		for i, l := range lines {
			newLines = append(newLines, "["+strconv.Itoa(i+1)+"] "+l)
		}

		fileIndex := FileIndex{Blob: blob, Metadata: []Metadata{Metadata{Project: project, Repo: repo, Refs: branch, Path: filePath, Ext: ext}}, Content: strings.Join(newLines, "\n")}

		_, err := esi.client.Index().
			Index("gosource").
			Type("file").
			Id(blob).
			BodyJson(fileIndex).
			Refresh(true).
			Do()

		if err != nil {
			log.Println("Add Doc error", err)
			return err
		}
	}

	log.Println("Indexed!")
	return nil
}

func (esi *ESIndexer) SearchQuery(query string) []Hit {
	// termQuery := elastic.NewTermsQuery("content", strings.Split(query, " "))
	q := elastic.NewQueryStringQuery(query).DefaultField("content").DefaultOperator("AND")
	searchResult, err := esi.client.Search().
		Index("gosource"). // search in index "twitter"
		FetchSourceContext(elastic.NewFetchSourceContext(true).Include("blob", "metadata")).
		Query(q). // specify the query
		Highlight(elastic.NewHighlight().Field("content").PreTags("@GITK_MARK_PRE@").PostTags("@GITK_MARK_POST@")).
		Sort("metadata.path", true). // sort by "user" field, ascending
		From(0).Size(10).            // take documents 0-9
		Pretty(true).                // pretty print request and response JSON
		Do()                         // execute

	if err != nil {
		log.Println("error", err)
		return []Hit{}
	}

	list := []Hit{}
	if searchResult.Hits.TotalHits > 0 {
		for _, hit := range searchResult.Hits.Hits {
			// hit.Index contains the name of the index

			// Deserialize hit.Source into a Tweet (could also be just a map[string]interface{}).
			var s Source
			json.Unmarshal(*hit.Source, &s)

			hsList := []HighlightSource{}
			for _, hc := range hit.Highlight["content"] {
				list := []string{}
				first := 0
				for _, l := range strings.Split(hc, "\n") {
					groups := LINE_TAG.FindAllStringSubmatch(l, 1)
					if len(groups) == 1 {
						if first == 0 {
							if strings.TrimSpace(groups[0][2]) != "" {
								first, _ = strconv.Atoi(groups[0][1])
								list = append(list, groups[0][2])
							}
						} else {
							list = append(list, groups[0][2])
						}
					}
				}
				hs := HighlightSource{Offset: first, Content: strings.Join(list, "\n")}
				hsList = append(hsList, hs)
			}

			h := Hit{Source: s, Highlight: hsList}
			list = append(list, h)
		}
	}
	return list
}

func find(f func(s Metadata, i int) bool, s []Metadata) *Metadata {
	for index, x := range s {
		if f(x, index) == true {
			return &x
		}
	}
	return nil
}

func filter(f func(s Metadata, i int) bool, s []Metadata) []Metadata {
	ans := make([]Metadata, 0)
	for index, x := range s {
		if f(x, index) == true {
			ans = append(ans, x)
		}
	}
	return ans
}
