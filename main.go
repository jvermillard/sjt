package main

import (
    "crypto/tls"
    "encoding/json"
    "fmt"
    "io/ioutil"
    "log"
    "net/http"
    "os"
    "net/url"

)

func main() {
    fmt.Println("Stash to Jenkins")

    stashUrl := os.Args[1]
    stashUser := os.Args[2]
    stashPwd := os.Args[3]

    project := os.Args[4]

    repo := os.Args[5]

    jenkinsUrl := os.Args[6]
    jenkinsUser := os.Args[7]
    jenkinsPwd := os.Args[8]

    job := os.Args[9]

    parameter := os.Args[10]

    //...fuck crapy certificate
    tr := &http.Transport{
        TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
    }

    client := &http.Client{Transport: tr}

    // get the list of open pull requests

    req, err := http.NewRequest("GET", stashUrl+"/rest/api/1.0/projects/"+project+"/repos/"+repo+"/pull-requests", nil)

    if err != nil {
        log.Fatal(err)
    }

    req.SetBasicAuth(stashUser, stashPwd)

    resp, err := client.Do(req)

    if err != nil {
        log.Fatal(err)
    }

    body, err := ioutil.ReadAll(resp.Body)

    //fmt.Println(string(body))

    if err != nil {
        log.Fatal(err)
    }

    var pr struct {
        Size   uint
        Limit  uint
        Values []struct {
            Id      int
            Open    bool
            Closed  bool
            FromRef struct {
                DisplayId       string
                LatestChangeset string
            }
        }
    }

    err = json.Unmarshal(body, &pr)

    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(pr.Size)
    for _, v := range pr.Values {
        fmt.Printf("PR: %s, id=%d\n", v.FromRef.DisplayId, v.Id)
        fmt.Printf("Last commit: %s\n\n", v.FromRef.LatestChangeset)

        req, err := http.NewRequest("POST", jenkinsUrl+"/job/"+job+"/build"+"?json="+url.QueryEscape(fmt.Sprintf("{\"parameter\": [{\"name\": \"%s\", \"value\": \"origin/%s\"}], \"\":\"\"}", parameter, v.FromRef.DisplayId)), nil)

        if err != nil {
            log.Fatal(err)
        }

        req.SetBasicAuth(jenkinsUser, jenkinsPwd)
        req.Header.Add("Content-Type", "application/json")


        client := &http.Client{}

        resp, err := client.Do(req)

        if err != nil {
            log.Fatal(err)
        }
        if resp.StatusCode != 201{
            body, err := ioutil.ReadAll(resp.Body)
            if err != nil {
                log.Fatal(err)
            }
            fmt.Println(string(body))
            log.Fatal(resp.Status)
        }

    }
}
