package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"
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
	lastCommit := make(map[int]string)

	for {

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

			// does this commit was built?
			commit := lastCommit[v.Id]

			if commit == "" || commit != v.FromRef.LatestChangeset {
				// trigger the build
				err = triggerBuild(jenkinsUrl, jenkinsUser, jenkinsPwd, job, v.FromRef.DisplayId, parameter)

				if err != nil {
					log.Fatal(err)
				}

				// save the last build commit SHA1
				lastCommit[v.Id] = v.FromRef.LatestChangeset
			}
		}

		time.Sleep(time.Duration(1) * time.Minute)
	}
}

//
// in charge of triggering a parametric job on jenkins
//
func triggerBuild(jenkinsUrl string, jenkinsUser string, jenkinsPwd string, job string, branch string, parameter string) error {
	req, err := http.NewRequest("POST", jenkinsUrl+"/job/"+job+"/build"+
		"?json="+url.QueryEscape(fmt.Sprintf("{\"parameter\": [{\"name\": \"%s\", \"value\": \"origin/%s\"}], \"\":\"\"}", parameter, branch)), nil)

	if err != nil {
		return err
	}

	req.SetBasicAuth(jenkinsUser, jenkinsPwd)
	req.Header.Add("Content-Type", "application/json")

	client := &http.Client{}

	resp, err := client.Do(req)

	if err != nil {
		log.Fatal(err)
	}
	if resp.StatusCode != 201 {
		body, err := ioutil.ReadAll(resp.Body)

		if err != nil {
			return err
		}

		fmt.Println(string(body))
		log.Fatal(resp.Status)
	}
	return nil
}
