package eventstream

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestApiResponseUnmarshal(t *testing.T) {
	tests := []struct {
		name     string
		jsonData string
		isError  bool
		expected any
	}{
		{
			name:     "invalid-json",
			jsonData: `{":}"`,
			isError:  true,
		},
		{
			name:     "unknown-type",
			jsonData: `{"type": "ihopethisstringwillneverappearinicinga2asavalidtype"}`,
			isError:  true,
		},
		{
			name:     "statechange-host-valid",
			jsonData: `{"acknowledgement":false,"check_result":{"active":true,"check_source":"docker-master","command":["/bin/bash","-c","/usr/games/fortune; exit $0","2"],"execution_end":1697188278.202986,"execution_start":1697188278.194409,"exit_status":2,"output":"If two people love each other, there can be no happy end to it.\n\t\t-- Ernest Hemingway","performance_data":[],"previous_hard_state":99,"schedule_end":1697188278.203036,"schedule_start":1697188278.1938322,"scheduling_source":"docker-master","state":2,"ttl":0,"type":"CheckResult","vars_after":{"attempt":2,"reachable":true,"state":2,"state_type":0},"vars_before":{"attempt":1,"reachable":true,"state":2,"state_type":0}},"downtime_depth":0,"host":"dummy-158","state":1,"state_type":0,"timestamp":1697188278.203504,"type":"StateChange"}`,
			expected: StateChange{
				Timestamp: Icinga2Time{time.UnixMicro(1697188278203504)},
				Host:      "dummy-158",
				State:     1,
				StateType: 0,
				CheckResult: CheckResult{
					ExitStatus: 2,
					Output:     "If two people love each other, there can be no happy end to it.\n\t\t-- Ernest Hemingway",
				},
				DowntimeDepth:   0,
				Acknowledgement: false,
			},
		},
		{
			name:     "statechange-service-valid",
			jsonData: `{"acknowledgement":false,"check_result":{"active":true,"check_source":"docker-master","command":["/bin/bash","-c","/usr/games/fortune; exit $0","2"],"execution_end":1697184778.611465,"execution_start":1697184778.600973,"exit_status":2,"output":"You're growing out of some of your problems, but there are others that\nyou're growing into.","performance_data":[],"previous_hard_state":0,"schedule_end":1697184778.611557,"schedule_start":1697184778.6,"scheduling_source":"docker-master","state":2,"ttl":0,"type":"CheckResult","vars_after":{"attempt":2,"reachable":false,"state":2,"state_type":0},"vars_before":{"attempt":1,"reachable":false,"state":2,"state_type":0}},"downtime_depth":0,"host":"dummy-280","service":"random fortune","state":2,"state_type":0,"timestamp":1697184778.612108,"type":"StateChange"}`,
			expected: StateChange{
				Timestamp: Icinga2Time{time.UnixMicro(1697184778612108)},
				Host:      "dummy-280",
				Service:   "random fortune",
				State:     2,
				StateType: 0,
				CheckResult: CheckResult{
					ExitStatus: 2,
					Output:     "You're growing out of some of your problems, but there are others that\nyou're growing into.",
				},
				DowntimeDepth:   0,
				Acknowledgement: false,
			},
		},
		{
			name:     "acknowledgementset-host",
			jsonData: `{"acknowledgement_type":1,"author":"icingaadmin","comment":"working on it","expiry":0,"host":"dummy-805","notify":true,"persistent":false,"state":1,"state_type":1,"timestamp":1697201074.579106,"type":"AcknowledgementSet"}`,
			expected: AcknowledgementSet{
				Timestamp: Icinga2Time{time.UnixMicro(1697201074579106)},
				Host:      "dummy-805",
				State:     1,
				StateType: 1,
				Author:    "icingaadmin",
				Comment:   "working on it",
			},
		},
		{
			name:     "acknowledgementset-service",
			jsonData: `{"acknowledgement_type":1,"author":"icingaadmin","comment":"will be fixed soon","expiry":0,"host":"docker-master","notify":true,"persistent":false,"service":"ssh","state":2,"state_type":1,"timestamp":1697201107.64792,"type":"AcknowledgementSet"}`,
			expected: AcknowledgementSet{
				Timestamp: Icinga2Time{time.UnixMicro(1697201107647920)},
				Host:      "docker-master",
				Service:   "ssh",
				State:     2,
				StateType: 1,
				Author:    "icingaadmin",
				Comment:   "will be fixed soon",
			},
		},
		{
			name:     "acknowledgementcleared-host",
			jsonData: `{"acknowledgement_type":0,"host":"dummy-805","state":1,"state_type":1,"timestamp":1697201082.440148,"type":"AcknowledgementCleared"}`,
			expected: AcknowledgementCleared{
				Timestamp: Icinga2Time{time.UnixMicro(1697201082440148)},
				Host:      "dummy-805",
				State:     1,
				StateType: 1,
			},
		},
		{
			name:     "acknowledgementcleared-service",
			jsonData: `{"acknowledgement_type":0,"host":"docker-master","service":"ssh","state":2,"state_type":1,"timestamp":1697201110.220349,"type":"AcknowledgementCleared"}`,
			expected: AcknowledgementCleared{
				Timestamp: Icinga2Time{time.UnixMicro(1697201110220349)},
				Host:      "docker-master",
				Service:   "ssh",
				State:     2,
				StateType: 1,
			},
		},
		{
			name:     "commentadded-host",
			jsonData: `{"comment":{"__name":"dummy-912!f653e951-2210-432d-bca6-e3719ea74ca3","author":"icingaadmin","entry_time":1697191791.097852,"entry_type":1,"expire_time":0,"host_name":"dummy-912","legacy_id":1,"name":"f653e951-2210-432d-bca6-e3719ea74ca3","package":"_api","persistent":false,"service_name":"","source_location":{"first_column":0,"first_line":1,"last_column":68,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/comments/dummy-912!f653e951-2210-432d-bca6-e3719ea74ca3.conf"},"sticky":false,"templates":["f653e951-2210-432d-bca6-e3719ea74ca3"],"text":"oh noes","type":"Comment","version":1697191791.097867,"zone":"master"},"timestamp":1697191791.099201,"type":"CommentAdded"}`,
			expected: CommentAdded{
				Timestamp: Icinga2Time{time.UnixMicro(1697191791099201)},
				Comment: Comment{
					Host:   "dummy-912",
					Author: "icingaadmin",
					Text:   "oh noes",
				},
			},
		},
		{
			name:     "commentadded-service",
			jsonData: `{"comment":{"__name":"dummy-912!ping4!8c00fb6a-5948-4249-a9d5-d1b6eb8945d0","author":"icingaadmin","entry_time":1697197990.035889,"entry_type":1,"expire_time":0,"host_name":"dummy-912","legacy_id":8,"name":"8c00fb6a-5948-4249-a9d5-d1b6eb8945d0","package":"_api","persistent":false,"service_name":"ping4","source_location":{"first_column":0,"first_line":1,"last_column":68,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/comments/dummy-912!ping4!8c00fb6a-5948-4249-a9d5-d1b6eb8945d0.conf"},"sticky":false,"templates":["8c00fb6a-5948-4249-a9d5-d1b6eb8945d0"],"text":"if in doubt, check ticket #23","type":"Comment","version":1697197990.035905,"zone":"master"},"timestamp":1697197990.037244,"type":"CommentAdded"}`,
			expected: CommentAdded{
				Timestamp: Icinga2Time{time.UnixMicro(1697197990037244)},
				Comment: Comment{
					Host:    "dummy-912",
					Service: "ping4",
					Author:  "icingaadmin",
					Text:    "if in doubt, check ticket #23",
				},
			},
		},
		{
			name:     "commentremoved-host",
			jsonData: `{"comment":{"__name":"dummy-912!f653e951-2210-432d-bca6-e3719ea74ca3","author":"icingaadmin","entry_time":1697191791.097852,"entry_type":1,"expire_time":0,"host_name":"dummy-912","legacy_id":1,"name":"f653e951-2210-432d-bca6-e3719ea74ca3","package":"_api","persistent":false,"service_name":"","source_location":{"first_column":0,"first_line":1,"last_column":68,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/comments/dummy-912!f653e951-2210-432d-bca6-e3719ea74ca3.conf"},"sticky":false,"templates":["f653e951-2210-432d-bca6-e3719ea74ca3"],"text":"oh noes","type":"Comment","version":1697191791.097867,"zone":"master"},"timestamp":1697191807.910093,"type":"CommentRemoved"}`,
			expected: CommentRemoved{
				Timestamp: Icinga2Time{time.UnixMicro(1697191807910093)},
				Comment: Comment{
					Host:   "dummy-912",
					Author: "icingaadmin",
					Text:   "oh noes",
				},
			},
		},
		{
			name:     "commentremoved-service",
			jsonData: `{"comment":{"__name":"dummy-912!ping4!8c00fb6a-5948-4249-a9d5-d1b6eb8945d0","author":"icingaadmin","entry_time":1697197990.035889,"entry_type":1,"expire_time":0,"host_name":"dummy-912","legacy_id":8,"name":"8c00fb6a-5948-4249-a9d5-d1b6eb8945d0","package":"_api","persistent":false,"service_name":"ping4","source_location":{"first_column":0,"first_line":1,"last_column":68,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/comments/dummy-912!ping4!8c00fb6a-5948-4249-a9d5-d1b6eb8945d0.conf"},"sticky":false,"templates":["8c00fb6a-5948-4249-a9d5-d1b6eb8945d0"],"text":"if in doubt, check ticket #23","type":"Comment","version":1697197990.035905,"zone":"master"},"timestamp":1697197996.584392,"type":"CommentRemoved"}`,
			expected: CommentRemoved{
				Timestamp: Icinga2Time{time.UnixMicro(1697197996584392)},
				Comment: Comment{
					Host:    "dummy-912",
					Service: "ping4",
					Author:  "icingaadmin",
					Text:    "if in doubt, check ticket #23",
				},
			},
		},
		{
			name:     "downtimeadded-host",
			jsonData: `{"downtime":{"__name":"dummy-157!e5d4d4ac-615a-4995-ab8f-09d9cd9503b1","author":"icingaadmin","authoritative_zone":"","comment":"updates","config_owner":"","config_owner_hash":"","duration":0,"end_time":1697210639,"entry_time":1697207050.509957,"fixed":true,"host_name":"dummy-157","legacy_id":3,"name":"e5d4d4ac-615a-4995-ab8f-09d9cd9503b1","package":"_api","parent":"","remove_time":0,"scheduled_by":"","service_name":"","source_location":{"first_column":0,"first_line":1,"last_column":69,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/downtimes/dummy-157!e5d4d4ac-615a-4995-ab8f-09d9cd9503b1.conf"},"start_time":1697207039,"templates":["e5d4d4ac-615a-4995-ab8f-09d9cd9503b1"],"trigger_time":0,"triggered_by":"","triggers":[],"type":"Downtime","version":1697207050.509971,"zone":"master"},"timestamp":1697207050.511293,"type":"DowntimeAdded"}`,
			expected: DowntimeAdded{
				Timestamp: Icinga2Time{time.UnixMicro(1697207050511293)},
				Downtime: Downtime{
					Host:    "dummy-157",
					Author:  "icingaadmin",
					Comment: "updates",
				},
			},
		},
		{
			name:     "downtimeadded-service",
			jsonData: `{"downtime":{"__name":"docker-master!http!3dabe7e7-32b2-4112-ba8f-a6567e5be79f","author":"icingaadmin","authoritative_zone":"","comment":"broken until Monday","config_owner":"","config_owner_hash":"","duration":0,"end_time":1697210716,"entry_time":1697207141.216009,"fixed":true,"host_name":"docker-master","legacy_id":4,"name":"3dabe7e7-32b2-4112-ba8f-a6567e5be79f","package":"_api","parent":"","remove_time":0,"scheduled_by":"","service_name":"http","source_location":{"first_column":0,"first_line":1,"last_column":69,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/downtimes/docker-master!http!3dabe7e7-32b2-4112-ba8f-a6567e5be79f.conf"},"start_time":1697207116,"templates":["3dabe7e7-32b2-4112-ba8f-a6567e5be79f"],"trigger_time":0,"triggered_by":"","triggers":[],"type":"Downtime","version":1697207141.216025,"zone":""},"timestamp":1697207141.217425,"type":"DowntimeAdded"}`,
			expected: DowntimeAdded{
				Timestamp: Icinga2Time{time.UnixMicro(1697207141217425)},
				Downtime: Downtime{
					Host:    "docker-master",
					Service: "http",
					Author:  "icingaadmin",
					Comment: "broken until Monday",
				},
			},
		},
		{
			name:     "downtimestarted-host",
			jsonData: `{"downtime":{"__name":"dummy-157!e5d4d4ac-615a-4995-ab8f-09d9cd9503b1","author":"icingaadmin","authoritative_zone":"","comment":"updates","config_owner":"","config_owner_hash":"","duration":0,"end_time":1697210639,"entry_time":1697207050.509957,"fixed":true,"host_name":"dummy-157","legacy_id":3,"name":"e5d4d4ac-615a-4995-ab8f-09d9cd9503b1","package":"_api","parent":"","remove_time":0,"scheduled_by":"","service_name":"","source_location":{"first_column":0,"first_line":1,"last_column":69,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/downtimes/dummy-157!e5d4d4ac-615a-4995-ab8f-09d9cd9503b1.conf"},"start_time":1697207039,"templates":["e5d4d4ac-615a-4995-ab8f-09d9cd9503b1"],"trigger_time":0,"triggered_by":"","triggers":[],"type":"Downtime","version":1697207050.509971,"zone":"master"},"timestamp":1697207050.511378,"type":"DowntimeStarted"}`,
			expected: DowntimeStarted{
				Timestamp: Icinga2Time{time.UnixMicro(1697207050511378)},
				Downtime: Downtime{
					Host:    "dummy-157",
					Author:  "icingaadmin",
					Comment: "updates",
				},
			},
		},
		{
			name:     "downtimestarted-service",
			jsonData: `{"downtime":{"__name":"docker-master!http!3dabe7e7-32b2-4112-ba8f-a6567e5be79f","author":"icingaadmin","authoritative_zone":"","comment":"broken until Monday","config_owner":"","config_owner_hash":"","duration":0,"end_time":1697210716,"entry_time":1697207141.216009,"fixed":true,"host_name":"docker-master","legacy_id":4,"name":"3dabe7e7-32b2-4112-ba8f-a6567e5be79f","package":"_api","parent":"","remove_time":0,"scheduled_by":"","service_name":"http","source_location":{"first_column":0,"first_line":1,"last_column":69,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/downtimes/docker-master!http!3dabe7e7-32b2-4112-ba8f-a6567e5be79f.conf"},"start_time":1697207116,"templates":["3dabe7e7-32b2-4112-ba8f-a6567e5be79f"],"trigger_time":0,"triggered_by":"","triggers":[],"type":"Downtime","version":1697207141.216025,"zone":""},"timestamp":1697207141.217507,"type":"DowntimeStarted"}`,
			expected: DowntimeStarted{
				Timestamp: Icinga2Time{time.UnixMicro(1697207141217507)},
				Downtime: Downtime{
					Host:    "docker-master",
					Service: "http",
					Author:  "icingaadmin",
					Comment: "broken until Monday",
				},
			},
		},
		{
			name:     "downtimetriggered-host",
			jsonData: `{"downtime":{"__name":"dummy-157!e5d4d4ac-615a-4995-ab8f-09d9cd9503b1","author":"icingaadmin","authoritative_zone":"","comment":"updates","config_owner":"","config_owner_hash":"","duration":0,"end_time":1697210639,"entry_time":1697207050.509957,"fixed":true,"host_name":"dummy-157","legacy_id":3,"name":"e5d4d4ac-615a-4995-ab8f-09d9cd9503b1","package":"_api","parent":"","remove_time":0,"scheduled_by":"","service_name":"","source_location":{"first_column":0,"first_line":1,"last_column":69,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/downtimes/dummy-157!e5d4d4ac-615a-4995-ab8f-09d9cd9503b1.conf"},"start_time":1697207039,"templates":["e5d4d4ac-615a-4995-ab8f-09d9cd9503b1"],"trigger_time":1697207050.509957,"triggered_by":"","triggers":[],"type":"Downtime","version":1697207050.509971,"zone":"master"},"timestamp":1697207050.511608,"type":"DowntimeTriggered"}`,
			expected: DowntimeTriggered{
				Timestamp: Icinga2Time{time.UnixMicro(1697207050511608)},
				Downtime: Downtime{
					Host:    "dummy-157",
					Author:  "icingaadmin",
					Comment: "updates",
				},
			},
		},
		{
			name:     "downtimetriggered-service",
			jsonData: `{"downtime":{"__name":"docker-master!http!3dabe7e7-32b2-4112-ba8f-a6567e5be79f","author":"icingaadmin","authoritative_zone":"","comment":"broken until Monday","config_owner":"","config_owner_hash":"","duration":0,"end_time":1697210716,"entry_time":1697207141.216009,"fixed":true,"host_name":"docker-master","legacy_id":4,"name":"3dabe7e7-32b2-4112-ba8f-a6567e5be79f","package":"_api","parent":"","remove_time":0,"scheduled_by":"","service_name":"http","source_location":{"first_column":0,"first_line":1,"last_column":69,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/downtimes/docker-master!http!3dabe7e7-32b2-4112-ba8f-a6567e5be79f.conf"},"start_time":1697207116,"templates":["3dabe7e7-32b2-4112-ba8f-a6567e5be79f"],"trigger_time":1697207141.216009,"triggered_by":"","triggers":[],"type":"Downtime","version":1697207141.216025,"zone":""},"timestamp":1697207141.217726,"type":"DowntimeTriggered"}`,
			expected: DowntimeTriggered{
				Timestamp: Icinga2Time{time.UnixMicro(1697207141217726)},
				Downtime: Downtime{
					Host:    "docker-master",
					Service: "http",
					Author:  "icingaadmin",
					Comment: "broken until Monday",
				},
			},
		},
		{
			name:     "downtimeremoved-host",
			jsonData: `{"downtime":{"__name":"dummy-157!e5d4d4ac-615a-4995-ab8f-09d9cd9503b1","author":"icingaadmin","authoritative_zone":"","comment":"updates","config_owner":"","config_owner_hash":"","duration":0,"end_time":1697210639,"entry_time":1697207050.509957,"fixed":true,"host_name":"dummy-157","legacy_id":3,"name":"e5d4d4ac-615a-4995-ab8f-09d9cd9503b1","package":"_api","parent":"","remove_time":1697207096.187718,"scheduled_by":"","service_name":"","source_location":{"first_column":0,"first_line":1,"last_column":69,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/downtimes/dummy-157!e5d4d4ac-615a-4995-ab8f-09d9cd9503b1.conf"},"start_time":1697207039,"templates":["e5d4d4ac-615a-4995-ab8f-09d9cd9503b1"],"trigger_time":1697207050.509957,"triggered_by":"","triggers":[],"type":"Downtime","version":1697207050.509971,"zone":"master"},"timestamp":1697207096.187866,"type":"DowntimeRemoved"}`,
			expected: DowntimeRemoved{
				Timestamp: Icinga2Time{time.UnixMicro(1697207096187866)},
				Downtime: Downtime{
					Host:    "dummy-157",
					Author:  "icingaadmin",
					Comment: "updates",
				},
			},
		},
		{
			name:     "downtimeremoved-service",
			jsonData: `{"downtime":{"__name":"docker-master!http!3dabe7e7-32b2-4112-ba8f-a6567e5be79f","author":"icingaadmin","authoritative_zone":"","comment":"broken until Monday","config_owner":"","config_owner_hash":"","duration":0,"end_time":1697210716,"entry_time":1697207141.216009,"fixed":true,"host_name":"docker-master","legacy_id":4,"name":"3dabe7e7-32b2-4112-ba8f-a6567e5be79f","package":"_api","parent":"","remove_time":1697207144.746117,"scheduled_by":"","service_name":"http","source_location":{"first_column":0,"first_line":1,"last_column":69,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/downtimes/docker-master!http!3dabe7e7-32b2-4112-ba8f-a6567e5be79f.conf"},"start_time":1697207116,"templates":["3dabe7e7-32b2-4112-ba8f-a6567e5be79f"],"trigger_time":1697207141.216009,"triggered_by":"","triggers":[],"type":"Downtime","version":1697207141.216025,"zone":""},"timestamp":1697207144.746333,"type":"DowntimeRemoved"}`,
			expected: DowntimeRemoved{
				Timestamp: Icinga2Time{time.UnixMicro(1697207144746333)},
				Downtime: Downtime{
					Host:    "docker-master",
					Service: "http",
					Author:  "icingaadmin",
					Comment: "broken until Monday",
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resp, err := UnmarshalEventStreamResponse([]byte(test.jsonData))
			if (err != nil) != test.isError {
				t.Errorf("unexpected error state; got error: %t, expected: %t", err != nil, test.isError)
				return
			}
			if err != nil {
				if !test.isError {
					t.Error(err)
				}
				return
			}

			if !reflect.DeepEqual(resp, test.expected) {
				t.Logf("got:      %#v", resp)
				t.Logf("expected: %#v", test.expected)
				t.Error("unexpected response")
			}
		})
	}
}

func TestIcinga2Time(t *testing.T) {
	tests := []struct {
		name     string
		jsonData string
		isError  bool
		expected Icinga2Time
	}{
		{
			name:     "json-empty",
			jsonData: "",
			isError:  true,
		},
		{
			name:     "json-invalid",
			jsonData: "{",
			isError:  true,
		},
		{
			name:     "json-wrong-type",
			jsonData: `"AAA"`,
			isError:  true,
		},
		{
			name:     "epoch-time",
			jsonData: "0.0",
			expected: Icinga2Time{time.Date(1970, time.January, 1, 0, 0, 0, 0, time.UTC)},
		},
		{
			name:     "example-time",
			jsonData: "1697207144.746333",
			expected: Icinga2Time{time.Date(2023, time.October, 13, 14, 25, 44, 746333000, time.UTC)},
		},
		{
			name:     "example-time-location",
			jsonData: "1697207144.746333",
			expected: Icinga2Time{time.Date(2023, time.October, 13, 16, 25, 44, 746333000,
				time.FixedZone("Europe/Berlin summer", 2*60*60))},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var ici2time Icinga2Time
			err := json.Unmarshal([]byte(test.jsonData), &ici2time)
			if (err != nil) != test.isError {
				t.Errorf("unexpected error state; got error: %t, expected: %t", err != nil, test.isError)
				return
			}
			if err != nil {
				if !test.isError {
					t.Error(err)
				}
				return
			}

			if ici2time.Compare(test.expected.Time) != 0 {
				t.Logf("got:      %#v", ici2time)
				t.Logf("expected: %#v", test.expected)
				t.Error("unexpected response")
			}
		})
	}
}
