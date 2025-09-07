
package main


import (
    "context"
    "encoding/xml"
    "html/template"
    "log"
    "net/http"
    "os"
    "sync"
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/credentials"
    "github.com/aws/aws-sdk-go-v2/service/s3"
    "io"
    "sort"
    "github.com/joho/godotenv"
    "compress/gzip"
)


// Download the latest timetable XML from S3 and print the first 500 bytes
func downloadLatestTimetableFromS3() {
    bucket := "darwin.xmltimetable"
    prefix := "PPTimetable/"
    region := "eu-west-1"
	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	log.Printf("Using AWS_ACCESS_KEY_ID: %s", accessKey)



    if accessKey == "" || secretKey == "" {
        log.Println("AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY must be set in environment.")
        return
    }

    ctx := context.Background()
    cfg, err := config.LoadDefaultConfig(ctx,
        config.WithRegion(region),
        config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
    )
    if err != nil {
        log.Printf("Failed to load AWS config: %v", err)
        return
    }
    client := s3.NewFromConfig(cfg)

    // List objects with the prefix
    listOut, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
        Bucket: &bucket,
        Prefix: &prefix,
    })
    if err != nil {
        log.Printf("Failed to list S3 objects: %v", err)
        return
    }
    if len(listOut.Contents) == 0 {
        log.Println("No timetable files found in S3 bucket.")
        return
    }

    // Find the latest file by LastModified
    sort.Slice(listOut.Contents, func(i, j int) bool {
        return listOut.Contents[i].LastModified.After(*listOut.Contents[j].LastModified)
    })
    latest := listOut.Contents[0]
    log.Printf("Downloading latest timetable: %s", *latest.Key)

    getOut, err := client.GetObject(ctx, &s3.GetObjectInput{
        Bucket: &bucket,
        Key:    latest.Key,
    })
    if err != nil {
        log.Printf("Failed to download S3 object: %v", err)
        return
    }
    defer getOut.Body.Close()

    gz, err := gzip.NewReader(getOut.Body)
    if err != nil {
        log.Printf("Failed to ungzip S3 object: %v", err)
        return
    }
    defer gz.Close()

    buf := make([]byte, 1000)
    n, err := gz.Read(buf)
    if err != nil && err != io.EOF {
        log.Printf("Failed to read ungzipped S3 object: %v", err)
        return
    }
    log.Printf("First 1000 bytes of ungzipped timetable file:\n%s", string(buf[:n]))
}

// Template for the main page
var pageTmpl = template.Must(template.New("page").Parse(`
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>Train Route Progression</title>
    <script src="https://unpkg.com/htmx.org@1.9.10"></script>
</head>
<body>
    <h1>Train Route Progression</h1>
    <div id="train-progression" hx-get="/progress" hx-trigger="load, every 30s" hx-swap="innerHTML">
        <p>Loading train route...</p>
    </div>
</body>
</html>
`))

// Template for the train progress (htmx partial)
var progressTmpl = template.Must(template.New("progress").Parse(`
<h2>Train 2B15 Progress</h2>
<ul>
    {{range .Stops}}
        <li>
            <strong>{{.Station}}</strong>: 
            Scheduled {{.Scheduled}} | Actual {{.Actual}} | Status: {{.Status}}
        </li>
    {{end}}
</ul>
`))

// Data structures for train progress
type Stop struct {
    Station   string
    Scheduled string
    Actual    string
    Status    string
}
type TrainProgress struct {
    Stops []Stop
}


// Shared cache for train progress (real data)
var (
    train2B15Cache = TrainProgress{}
    train2B15Mu    sync.RWMutex
)

// Darwin XML structs (simplified for TS)
type DarwinPport struct {
    XMLName xml.Name   `xml:"Pport"`
    TS      []DarwinTS `xml:"TS"`
}
type DarwinTS struct {
    RID     string        `xml:"rid,attr"`
    UID     string        `xml:"uid,attr"`
    TrainID string        `xml:"trainid,attr"`
    Locs    []DarwinLoc   `xml:"Location"`
}
type DarwinLoc struct {
    Tiploc string `xml:"tpl,attr"`
    Pta    string `xml:"pta,attr"`
    Ata    string `xml:"ata,attr"`
    Act    string `xml:"act,attr"`
}

func fetchTrain2B15Progress() TrainProgress {
    train2B15Mu.RLock()
    defer train2B15Mu.RUnlock()
    return train2B15Cache
}

func main() {
    // Load environment variables from .env file
    _ = godotenv.Load()

	log.Println(CancellationReasons[100]) // Example usage of the imported package

    // Download and print the latest timetable XML from S3 at startup
    downloadLatestTimetableFromS3()

    // Use environment variables for Darwin credentials
    username := os.Getenv("DARWIN_USERNAME")
    password := os.Getenv("DARWIN_TOKEN")
    if username == "" || password == "" {
        log.Fatal("Please set DARWIN_USERNAME and DARWIN_TOKEN environment variables.")
    }
    
    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        if err := pageTmpl.Execute(w, nil); err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
        }
    })

    http.HandleFunc("/progress", func(w http.ResponseWriter, r *http.Request) {
		log.Println("Serving /progress")
        progress := fetchTrain2B15Progress()
        if err := progressTmpl.Execute(w, progress); err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
        }
    })

    log.Println("Server started at http://localhost:8081")
    log.Fatal(http.ListenAndServe(":8081", nil))
}