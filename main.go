package main

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
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

	var state struct {
		// get the list of open pull requests
		LastCommitForPr map[string]string
		// pull requests by branch
		PullRequestByBranch map[string]int
		// list of built already reported to stash
		CommentedBuilds map[string]bool
	}

	// load state.json
	fileBody, err := ioutil.ReadFile("state.json")

	if err == nil {
		fmt.Println("loading state file")
		err := json.Unmarshal(fileBody, &state)
		if err != nil {
			panic(err)
		}
	} else {
		fmt.Println("can't load the state file: " + err.Error())

		state.LastCommitForPr = make(map[string]string)
		state.PullRequestByBranch = make(map[string]int)
		state.CommentedBuilds = make(map[string]bool)
	}

	for {
		// look for builds to report

		fmt.Println("list builds")
		builds, err := listBuilds(jenkinsUrl, jenkinsUser, jenkinsPwd, job)
		if err != nil {
			panic(err)
		}

		// for each builds check if we already pushed the status

		for _, b := range builds {
			fmt.Println(b)

			fmt.Println("get git info")

			branch, sha1, err := getGitInfo(jenkinsUrl, jenkinsUser, jenkinsPwd, job, b)
			if err != nil {
				panic(err)
			}

			branch = strings.Replace(branch, "origin/", "", -1)

			status, tests, err := getStatus(jenkinsUrl, jenkinsUser, jenkinsPwd, job, b)
			if err != nil {
				panic(err)
			}

			// skip building
			if status == "" {
				continue
			}

			fmt.Println("job res: " + branch + " " + sha1 + " " + status + " " + tests)

			// does the build was already reported?
			_, found := state.CommentedBuilds[branch+"#"+sha1]

			if !found {
				// post stash comment

				idPr, f := state.PullRequestByBranch[branch]

				if f {
					fmt.Println("posting comment : Integration build result for branch " + branch + " (commit: " + sha1 + ")\n status: " + status + " tests: " + tests)
					req, err := http.NewRequest("POST", stashUrl+"/rest/api/1.0/projects/"+project+"/repos/"+repo+"/pull-requests/"+strconv.Itoa(idPr)+"/comments",
						strings.NewReader("{ \"text\" : \"Integration build result for branch: "+branch+", build: #"+strconv.Itoa(b)+", commit: "+sha1+"\\n status: "+status+", tests: "+tests+"\"}"))

					if err != nil {
						panic(err)
					}

					req.Header.Add("Content-Type", "application/json")
					req.SetBasicAuth(stashUser, stashPwd)

					resp, err := client.Do(req)

					if err != nil {
						panic(err)
					}

					if resp.StatusCode != 201 {
						body, err := ioutil.ReadAll(resp.Body)

						if err != nil {
							panic(err)
						}
						fmt.Printf("status: %s, raw %s \n"+resp.Status, body)
					}

					state.CommentedBuilds[branch+"#"+sha1] = true
				} else {
					fmt.Println("No pull request for this branch build: " + branch)
				}
			}
		}

		resp, err := get(stashUrl+"/rest/api/1.0/projects/"+project+"/repos/"+repo+"/pull-requests", stashUser, stashPwd)
		if err != nil {
			panic(err)
		}

		body, err := ioutil.ReadAll(resp.Body)

		if err != nil {
			panic(err)
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
			panic(err)
		}

		fmt.Printf("Pull request count: %d\n", pr.Size)

		ids := make(map[int]struct{})

		for _, v := range pr.Values {
			fmt.Printf("PR: %s, id=%d\n", v.FromRef.DisplayId, v.Id)
			fmt.Printf("Last commit: %s\n\n", v.FromRef.LatestChangeset)

			state.PullRequestByBranch[v.FromRef.DisplayId] = v.Id
			ids[v.Id] = struct{}{}

			// does this commit was built?
			commit, found := state.LastCommitForPr[strconv.Itoa(v.Id)]
			if !found || commit != v.FromRef.LatestChangeset {
				// trigger the build
				err = triggerBuild(jenkinsUrl, jenkinsUser, jenkinsPwd, job, v.FromRef.DisplayId, parameter)

				if err != nil {
					panic(err)
				}

				// save the last build commit SHA1
				state.LastCommitForPr[strconv.Itoa(v.Id)] = v.FromRef.LatestChangeset
			}
		}

		// clean dead PR

		for k, _ := range state.LastCommitForPr {
			v, err := strconv.Atoi(k)
			if err != nil {
				panic(err)
			}

			_, p := ids[v]
			if !p {
				delete(state.LastCommitForPr, k)
			}
		}

		// save state
		content, err := json.Marshal(&state)

		if err != nil {
			panic(err)
		}

		err = ioutil.WriteFile("state.json", content, 0644)

		if err != nil {
			panic(err)
		}

		time.Sleep(time.Duration(1) * time.Minute)
	}
}

// get issue a get on the given url and return the http response and/or an error
func get(geturl string, user string, password string) (res *http.Response, err error) {
	fmt.Printf("GET url: %s\n", geturl)

	req, err := http.NewRequest("GET", geturl, nil)

	if err != nil {
		return nil, err
	}

	req.SetBasicAuth(user, password)

	//...fuck crapy certificate
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	client := &http.Client{Transport: tr}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

//
func triggerBuild(jenkinsUrl string, jenkinsUser string, jenkinsPwd string, job string, branch string, parameter string) error {
	fmt.Println("triggering build for branch :'" + branch + "'")

	postUrl := jenkinsUrl + "/job/" + job + "/build" +
		"?json=" + url.QueryEscape(fmt.Sprintf("{\"parameter\": [{\"name\": \"%s\", \"value\": \"%s\"}], \"\":\"\"}", parameter, branch))

	fmt.Println("POST URL : " + postUrl)
	req, err := http.NewRequest("POST", postUrl, nil)

	if err != nil {
		return err
	}

	req.SetBasicAuth(jenkinsUser, jenkinsPwd)
	req.Header.Add("Content-Type", "application/json")

	client := &http.Client{}

	resp, err := client.Do(req)

	if err != nil {
		return err
	}

	if resp.StatusCode != 201 {
		body, err := ioutil.ReadAll(resp.Body)

		if err != nil {
			return err
		}

		fmt.Println(string(body))
		return errors.New(resp.Status)
	}
	return nil
}

func listBuilds(jenkinsUrl string, jenkinsUser string, jenkinsPwd string, job string) ([]int, error) {

	resp, err := get(jenkinsUrl+"/job/"+job+"/api/json", jenkinsUser, jenkinsPwd)
	if err != nil {
		return nil, err
	}

	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		return nil, err
	}

	var builds struct {
		Builds []struct {
			Number int
		}
	}

	err = json.Unmarshal(body, &builds)
	if err != nil {
		return nil, err
	}

	size := len(builds.Builds)
	result := make([]int, size)

	fmt.Printf("number of builds : %d\n", size)
	for i := 0; i < size; i++ {
		fmt.Printf(" build n° %d\n", builds.Builds[i].Number)
		result[i] = builds.Builds[i].Number
	}
	return result, nil
}

func getGitInfo(jenkinsUrl string, jenkinsUser string, jenkinsPwd string, job string, build int) (branch string, sha1 string, err error) {
	fmt.Println(jenkinsUrl + "/job/" + job + "/" + strconv.Itoa(build) + "/git/api/json")

	resp, err := get(jenkinsUrl+"/job/"+job+"/"+strconv.Itoa(build)+"/git/api/json", jenkinsUser, jenkinsPwd)

	if err != nil {
		return "", "", err
	}

	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		return "", "", err
	}

	var git struct {
		LastBuiltRevision struct {
			Branch []struct {
				SHA1 string
				Name string
			}
		}
	}

	err = json.Unmarshal(body, &git)
	if err != nil {
		return "", "", err
	}

	if len(git.LastBuiltRevision.Branch) != 1 {
		return "", "", errors.New("weird")
	}

	return git.LastBuiltRevision.Branch[0].Name, git.LastBuiltRevision.Branch[0].SHA1, nil
}

func getStatus(jenkinsUrl string, jenkinsUser string, jenkinsPwd string, job string, build int) (status string, tests string, err error) {
	resp, err := get(jenkinsUrl+"/job/"+job+"/"+strconv.Itoa(build)+"/api/json", jenkinsUser, jenkinsPwd)

	if err != nil {
		return "", "", err
	}

	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		return "", "", err
	}

	var result struct {
		Result string
	}

	//fmt.Printf("RAW : %s\n", body)

	err = json.Unmarshal(body, &result)

	if err != nil {
		return "", "", err
	}

	resp, err = get(jenkinsUrl+"/job/"+job+"/"+strconv.Itoa(build)+"/testReport/api/json", jenkinsUser, jenkinsPwd)
	if err != nil {
		return "", "", err
	}

	body, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}

	if resp.StatusCode == 200 {
		var testRes struct {
			FailCount int
			PassCount int
			SkipCount int
		}
		err = json.Unmarshal(body, &testRes)
		if err != nil {
			return "", "", err
		}
		return result.Result, fmt.Sprintf("Fail: %d, Pass: %d, Skip: %d", testRes.FailCount, testRes.PassCount, testRes.SkipCount), nil
	} else {
		fmt.Printf("RAW JSON %s\n", body)
		return result.Result, "tests not passed", nil
	}
}
