package eventstream

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestIcinga2Time_UnmarshalJSON(t *testing.T) {
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
				t.Errorf("unexpected error state; got error: %t, expected: %t; %v", err != nil, test.isError, err)
				return
			} else if err != nil {
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

func TestObjectQueriesResult_UnmarshalJSON(t *testing.T) {
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
			name:     "invalid-typed-json",
			jsonData: `{"name": 23, "type": [], "attrs": null}`,
			isError:  true,
		},
		{
			name:     "unknown-type",
			jsonData: `{"type": "ihopethisstringwillneverappearinicinga2asavalidtype"}`,
			isError:  true,
		},
		{
			// $ curl -k -s -u root:icinga 'https://localhost:5665/v1/objects/comments' | jq -c '[.results[] | select(.attrs.service_name == "")][0]'
			name:     "comment-host",
			jsonData: `{"attrs":{"__name":"dummy-0!f1239b7d-6e13-4031-b7dd-4055fdd2cd80","active":true,"author":"icingaadmin","entry_time":1697454753.536457,"entry_type":1,"expire_time":0,"ha_mode":0,"host_name":"dummy-0","legacy_id":3,"name":"f1239b7d-6e13-4031-b7dd-4055fdd2cd80","original_attributes":null,"package":"_api","paused":false,"persistent":false,"service_name":"","source_location":{"first_column":0,"first_line":1,"last_column":68,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/comments/dummy-0!f1239b7d-6e13-4031-b7dd-4055fdd2cd80.conf"},"templates":["f1239b7d-6e13-4031-b7dd-4055fdd2cd80"],"text":"foo bar","type":"Comment","version":1697454753.53647,"zone":"master"},"joins":{},"meta":{},"name":"dummy-0!f1239b7d-6e13-4031-b7dd-4055fdd2cd80","type":"Comment"}`,
			expected: ObjectQueriesResult{
				Name: "dummy-0!f1239b7d-6e13-4031-b7dd-4055fdd2cd80",
				Type: "Comment",
				Attrs: &Comment{
					Host:      "dummy-0",
					Author:    "icingaadmin",
					Text:      "foo bar",
					EntryTime: Icinga2Time{time.UnixMicro(1697454753536457)},
					EntryType: 1,
				},
			},
		},
		{
			// $ curl -k -s -u root:icinga 'https://localhost:5665/v1/objects/comments' | jq -c '[.results[] | select(.attrs.service_name != "")][0]'
			name:     "comment-service",
			jsonData: `{"attrs":{"__name":"dummy-912!ping6!1b29580d-0a09-4265-ad1f-5e16f462443d","active":true,"author":"icingaadmin","entry_time":1697197701.307516,"entry_type":1,"expire_time":0,"ha_mode":0,"host_name":"dummy-912","legacy_id":1,"name":"1b29580d-0a09-4265-ad1f-5e16f462443d","original_attributes":null,"package":"_api","paused":false,"persistent":false,"service_name":"ping6","source_location":{"first_column":0,"first_line":1,"last_column":68,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/comments/dummy-912!ping6!1b29580d-0a09-4265-ad1f-5e16f462443d.conf"},"templates":["1b29580d-0a09-4265-ad1f-5e16f462443d"],"text":"adfadsfasdfasdf","type":"Comment","version":1697197701.307536,"zone":"master"},"joins":{},"meta":{},"name":"dummy-912!ping6!1b29580d-0a09-4265-ad1f-5e16f462443d","type":"Comment"}`,
			expected: ObjectQueriesResult{
				Name: "dummy-912!ping6!1b29580d-0a09-4265-ad1f-5e16f462443d",
				Type: "Comment",
				Attrs: &Comment{
					Host:      "dummy-912",
					Service:   "ping6",
					Author:    "icingaadmin",
					Text:      "adfadsfasdfasdf",
					EntryType: 1,
					EntryTime: Icinga2Time{time.UnixMicro(1697197701307516)},
				},
			},
		},
		{
			// $ curl -k -s -u root:icinga 'https://localhost:5665/v1/objects/downtimes' | jq -c '[.results[] | select(.attrs.service_name == "")][0]'
			name:     "downtime-host",
			jsonData: `{"attrs":{"__name":"dummy-11!af73f9d9-2ed8-45f8-b541-cce3f3fe0f6c","active":true,"author":"icingaadmin","authoritative_zone":"","comment":"turn down for what","config_owner":"","config_owner_hash":"","duration":0,"end_time":1698096240,"entry_time":1697456415.667442,"fixed":true,"ha_mode":0,"host_name":"dummy-11","legacy_id":2,"name":"af73f9d9-2ed8-45f8-b541-cce3f3fe0f6c","original_attributes":null,"package":"_api","parent":"","paused":false,"remove_time":0,"scheduled_by":"","service_name":"","source_location":{"first_column":0,"first_line":1,"last_column":69,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/downtimes/dummy-11!af73f9d9-2ed8-45f8-b541-cce3f3fe0f6c.conf"},"start_time":1697456292,"templates":["af73f9d9-2ed8-45f8-b541-cce3f3fe0f6c"],"trigger_time":1697456415.667442,"triggered_by":"","triggers":[],"type":"Downtime","version":1697456415.667458,"was_cancelled":false,"zone":"master"},"joins":{},"meta":{},"name":"dummy-11!af73f9d9-2ed8-45f8-b541-cce3f3fe0f6c","type":"Downtime"}`,
			expected: ObjectQueriesResult{
				Name: "dummy-11!af73f9d9-2ed8-45f8-b541-cce3f3fe0f6c",
				Type: "Downtime",
				Attrs: &Downtime{
					Host:    "dummy-11",
					Author:  "icingaadmin",
					Comment: "turn down for what",
				},
			},
		},
		{
			// $ curl -k -s -u root:icinga 'https://localhost:5665/v1/objects/downtimes' | jq -c '[.results[] | select(.attrs.service_name != "")][0]'
			name:     "downtime-service",
			jsonData: `{"attrs":{"__name":"docker-master!load!c27b27c2-e0ab-45ff-8b9b-e95f29851eb0","active":true,"author":"icingaadmin","authoritative_zone":"master","comment":"Scheduled downtime for backup","config_owner":"docker-master!load!backup-downtime","config_owner_hash":"ca9502dc8fa5d29c1cb2686808b5d2ccf3ea4a9c6dc3f3c09bfc54614c03c765","duration":0,"end_time":1697511600,"entry_time":1697439555.095232,"fixed":true,"ha_mode":0,"host_name":"docker-master","legacy_id":1,"name":"c27b27c2-e0ab-45ff-8b9b-e95f29851eb0","original_attributes":null,"package":"_api","parent":"","paused":false,"remove_time":0,"scheduled_by":"docker-master!load!backup-downtime","service_name":"load","source_location":{"first_column":0,"first_line":1,"last_column":69,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/downtimes/docker-master!load!c27b27c2-e0ab-45ff-8b9b-e95f29851eb0.conf"},"start_time":1697508000,"templates":["c27b27c2-e0ab-45ff-8b9b-e95f29851eb0"],"trigger_time":0,"triggered_by":"","triggers":[],"type":"Downtime","version":1697439555.095272,"was_cancelled":false,"zone":""},"joins":{},"meta":{},"name":"docker-master!load!c27b27c2-e0ab-45ff-8b9b-e95f29851eb0","type":"Downtime"}`,
			expected: ObjectQueriesResult{
				Name: "docker-master!load!c27b27c2-e0ab-45ff-8b9b-e95f29851eb0",
				Type: "Downtime",
				Attrs: &Downtime{
					Host:    "docker-master",
					Service: "load",
					Author:  "icingaadmin",
					Comment: "Scheduled downtime for backup",
				},
			},
		},
		{
			// $ curl -k -s -u root:icinga 'https://localhost:5665/v1/objects/hosts' | jq -c '.results[0]'
			name:     "host",
			jsonData: `{"attrs":{"__name":"dummy-244","acknowledgement":0,"acknowledgement_expiry":0,"acknowledgement_last_change":0,"action_url":"","active":true,"address":"127.0.0.1","address6":"::1","check_attempt":1,"check_command":"random fortune","check_interval":300,"check_period":"","check_timeout":null,"command_endpoint":"","display_name":"dummy-244","downtime_depth":0,"enable_active_checks":true,"enable_event_handler":true,"enable_flapping":false,"enable_notifications":true,"enable_passive_checks":true,"enable_perfdata":true,"event_command":"icinga-notifications-host-events","executions":null,"flapping":false,"flapping_current":0,"flapping_ignore_states":null,"flapping_last_change":0,"flapping_threshold":0,"flapping_threshold_high":30,"flapping_threshold_low":25,"force_next_check":false,"force_next_notification":false,"groups":["app-network","department-dev","env-qa","location-rome"],"ha_mode":0,"handled":false,"icon_image":"","icon_image_alt":"","last_check":1697459643.869006,"last_check_result":{"active":true,"check_source":"docker-master","command":["/bin/bash","-c","/usr/games/fortune; exit $0","0"],"execution_end":1697459643.868893,"execution_start":1697459643.863147,"exit_status":0,"output":"If you think last Tuesday was a drag, wait till you see what happens tomorrow!","performance_data":[],"previous_hard_state":99,"schedule_end":1697459643.869006,"schedule_start":1697459643.86287,"scheduling_source":"docker-master","state":0,"ttl":0,"type":"CheckResult","vars_after":{"attempt":1,"reachable":true,"state":0,"state_type":1},"vars_before":{"attempt":1,"reachable":true,"state":0,"state_type":1}},"last_hard_state":0,"last_hard_state_change":1697099900.637215,"last_reachable":true,"last_state":0,"last_state_change":1697099900.637215,"last_state_down":0,"last_state_type":1,"last_state_unreachable":0,"last_state_up":1697459643.868893,"max_check_attempts":3,"name":"dummy-244","next_check":1697459943.019035,"next_update":1697460243.031081,"notes":"","notes_url":"","original_attributes":null,"package":"_etc","paused":false,"previous_state_change":1697099900.637215,"problem":false,"retry_interval":60,"severity":0,"source_location":{"first_column":5,"first_line":2,"last_column":38,"last_line":2,"path":"/etc/icinga2/zones.d/master/03-dummys-hosts.conf"},"state":0,"state_type":1,"templates":["dummy-244","generic-icinga-notifications-host"],"type":"Host","vars":{"app":"network","department":"dev","env":"qa","is_dummy":true,"location":"rome"},"version":0,"volatile":false,"zone":"master"},"joins":{},"meta":{},"name":"dummy-244","type":"Host"}`,
			expected: ObjectQueriesResult{
				Name: "dummy-244",
				Type: "Host",
				Attrs: &HostServiceRuntimeAttributes{
					Name:   "dummy-244",
					Groups: []string{"app-network", "department-dev", "env-qa", "location-rome"},
					State:  0,
					LastCheckResult: CheckResult{
						ExitStatus: 0,
						Output:     "If you think last Tuesday was a drag, wait till you see what happens tomorrow!",
						State:      0,
						Command: []string{
							"/bin/bash",
							"-c",
							"/usr/games/fortune; exit $0",
							"0",
						},
						ExecutionStart: Icinga2Time{time.UnixMicro(1697459643863147)},
						ExecutionEnd:   Icinga2Time{time.UnixMicro(1697459643868893)},
					},
					LastStateChange:           Icinga2Time{time.UnixMicro(1697099900637215)},
					DowntimeDepth:             0,
					Acknowledgement:           0,
					AcknowledgementLastChange: Icinga2Time{time.UnixMicro(0)},
				},
			},
		},
		{
			// $ curl -k -s -u root:icinga -d '{"filter": "service.acknowledgement != 0"}' -H 'Accept: application/json' -H 'X-HTTP-Method-Override: GET' 'https://localhost:5665/v1/objects/services' | jq -c '.results[0]'
			name:     "service",
			jsonData: `{"attrs":{"__name":"docker-master!ssh","acknowledgement":1,"acknowledgement_expiry":0,"acknowledgement_last_change":1697460655.878141,"action_url":"","active":true,"check_attempt":1,"check_command":"ssh","check_interval":60,"check_period":"","check_timeout":null,"command_endpoint":"","display_name":"ssh","downtime_depth":0,"enable_active_checks":true,"enable_event_handler":true,"enable_flapping":false,"enable_notifications":true,"enable_passive_checks":true,"enable_perfdata":true,"event_command":"icinga-notifications-service-events","executions":null,"flapping":false,"flapping_current":0,"flapping_ignore_states":null,"flapping_last_change":0,"flapping_threshold":0,"flapping_threshold_high":30,"flapping_threshold_low":25,"force_next_check":false,"force_next_notification":false,"groups":[],"ha_mode":0,"handled":true,"host_name":"docker-master","icon_image":"","icon_image_alt":"","last_check":1697460711.134904,"last_check_result":{"active":true,"check_source":"docker-master","command":["/usr/lib/nagios/plugins/check_ssh","127.0.0.1"],"execution_end":1697460711.134875,"execution_start":1697460711.130247,"exit_status":2,"output":"connect to address 127.0.0.1 and port 22: Connection refused","performance_data":[],"previous_hard_state":99,"schedule_end":1697460711.134904,"schedule_start":1697460711.13,"scheduling_source":"docker-master","state":2,"ttl":0,"type":"CheckResult","vars_after":{"attempt":1,"reachable":true,"state":2,"state_type":1},"vars_before":{"attempt":1,"reachable":true,"state":2,"state_type":1}},"last_hard_state":2,"last_hard_state_change":1697099980.820806,"last_reachable":true,"last_state":2,"last_state_change":1697099896.120829,"last_state_critical":1697460711.134875,"last_state_ok":0,"last_state_type":1,"last_state_unknown":0,"last_state_unreachable":0,"last_state_warning":0,"max_check_attempts":5,"name":"ssh","next_check":1697460771.1299999,"next_update":1697460831.1397498,"notes":"","notes_url":"","original_attributes":null,"package":"_etc","paused":false,"previous_state_change":1697099896.120829,"problem":true,"retry_interval":30,"severity":640,"source_location":{"first_column":1,"first_line":47,"last_column":19,"last_line":47,"path":"/etc/icinga2/conf.d/services.conf"},"state":2,"state_type":1,"templates":["ssh","generic-icinga-notifications-service","generic-service"],"type":"Service","vars":null,"version":0,"volatile":false,"zone":""},"joins":{},"meta":{},"name":"docker-master!ssh","type":"Service"}`,
			expected: ObjectQueriesResult{
				Name: "docker-master!ssh",
				Type: "Service",
				Attrs: &HostServiceRuntimeAttributes{
					Name:   "ssh",
					Host:   "docker-master",
					Groups: []string{},
					State:  2,
					LastCheckResult: CheckResult{
						ExitStatus: 2,
						Output:     "connect to address 127.0.0.1 and port 22: Connection refused",
						State:      2,
						Command: []string{
							"/usr/lib/nagios/plugins/check_ssh",
							"127.0.0.1",
						},
						ExecutionStart: Icinga2Time{time.UnixMicro(1697460711130247)},
						ExecutionEnd:   Icinga2Time{time.UnixMicro(1697460711134875)},
					},
					LastStateChange:           Icinga2Time{time.UnixMicro(1697099896120829)},
					DowntimeDepth:             0,
					Acknowledgement:           1,
					AcknowledgementLastChange: Icinga2Time{time.UnixMicro(1697460655878141)},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var resp ObjectQueriesResult
			err := json.Unmarshal([]byte(test.jsonData), &resp)
			if (err != nil) != test.isError {
				t.Errorf("unexpected error state; got error: %t, expected: %t; %v", err != nil, test.isError, err)
				return
			} else if err != nil {
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
			expected: &StateChange{
				Timestamp: Icinga2Time{time.UnixMicro(1697188278203504)},
				Host:      "dummy-158",
				State:     1,
				StateType: 0,
				CheckResult: CheckResult{
					ExitStatus: 2,
					Output:     "If two people love each other, there can be no happy end to it.\n\t\t-- Ernest Hemingway",
					State:      2,
					Command: []string{
						"/bin/bash",
						"-c",
						"/usr/games/fortune; exit $0",
						"2",
					},
					ExecutionStart: Icinga2Time{time.UnixMicro(1697188278194409)},
					ExecutionEnd:   Icinga2Time{time.UnixMicro(1697188278202986)},
				},
				DowntimeDepth:   0,
				Acknowledgement: false,
			},
		},
		{
			name:     "statechange-service-valid",
			jsonData: `{"acknowledgement":false,"check_result":{"active":true,"check_source":"docker-master","command":["/bin/bash","-c","/usr/games/fortune; exit $0","2"],"execution_end":1697184778.611465,"execution_start":1697184778.600973,"exit_status":2,"output":"You're growing out of some of your problems, but there are others that\nyou're growing into.","performance_data":[],"previous_hard_state":0,"schedule_end":1697184778.611557,"schedule_start":1697184778.6,"scheduling_source":"docker-master","state":2,"ttl":0,"type":"CheckResult","vars_after":{"attempt":2,"reachable":false,"state":2,"state_type":0},"vars_before":{"attempt":1,"reachable":false,"state":2,"state_type":0}},"downtime_depth":0,"host":"dummy-280","service":"random fortune","state":2,"state_type":0,"timestamp":1697184778.612108,"type":"StateChange"}`,
			expected: &StateChange{
				Timestamp: Icinga2Time{time.UnixMicro(1697184778612108)},
				Host:      "dummy-280",
				Service:   "random fortune",
				State:     2,
				StateType: 0,
				CheckResult: CheckResult{
					ExitStatus: 2,
					Output:     "You're growing out of some of your problems, but there are others that\nyou're growing into.",
					State:      2,
					Command: []string{
						"/bin/bash",
						"-c",
						"/usr/games/fortune; exit $0",
						"2",
					},
					ExecutionStart: Icinga2Time{time.UnixMicro(1697184778600973)},
					ExecutionEnd:   Icinga2Time{time.UnixMicro(1697184778611465)},
				},
				DowntimeDepth:   0,
				Acknowledgement: false,
			},
		},
		{
			name:     "acknowledgementset-host",
			jsonData: `{"acknowledgement_type":1,"author":"icingaadmin","comment":"working on it","expiry":0,"host":"dummy-805","notify":true,"persistent":false,"state":1,"state_type":1,"timestamp":1697201074.579106,"type":"AcknowledgementSet"}`,
			expected: &AcknowledgementSet{
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
			expected: &AcknowledgementSet{
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
			expected: &AcknowledgementCleared{
				Timestamp: Icinga2Time{time.UnixMicro(1697201082440148)},
				Host:      "dummy-805",
				State:     1,
				StateType: 1,
			},
		},
		{
			name:     "acknowledgementcleared-service",
			jsonData: `{"acknowledgement_type":0,"host":"docker-master","service":"ssh","state":2,"state_type":1,"timestamp":1697201110.220349,"type":"AcknowledgementCleared"}`,
			expected: &AcknowledgementCleared{
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
			expected: &CommentAdded{
				Timestamp: Icinga2Time{time.UnixMicro(1697191791099201)},
				Comment: Comment{
					Host:      "dummy-912",
					Author:    "icingaadmin",
					Text:      "oh noes",
					EntryType: 1,
					EntryTime: Icinga2Time{time.UnixMicro(1697191791097852)},
				},
			},
		},
		{
			name:     "commentadded-service",
			jsonData: `{"comment":{"__name":"dummy-912!ping4!8c00fb6a-5948-4249-a9d5-d1b6eb8945d0","author":"icingaadmin","entry_time":1697197990.035889,"entry_type":1,"expire_time":0,"host_name":"dummy-912","legacy_id":8,"name":"8c00fb6a-5948-4249-a9d5-d1b6eb8945d0","package":"_api","persistent":false,"service_name":"ping4","source_location":{"first_column":0,"first_line":1,"last_column":68,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/comments/dummy-912!ping4!8c00fb6a-5948-4249-a9d5-d1b6eb8945d0.conf"},"sticky":false,"templates":["8c00fb6a-5948-4249-a9d5-d1b6eb8945d0"],"text":"if in doubt, check ticket #23","type":"Comment","version":1697197990.035905,"zone":"master"},"timestamp":1697197990.037244,"type":"CommentAdded"}`,
			expected: &CommentAdded{
				Timestamp: Icinga2Time{time.UnixMicro(1697197990037244)},
				Comment: Comment{
					Host:      "dummy-912",
					Service:   "ping4",
					Author:    "icingaadmin",
					Text:      "if in doubt, check ticket #23",
					EntryType: 1,
					EntryTime: Icinga2Time{time.UnixMicro(1697197990035889)},
				},
			},
		},
		{
			name:     "commentremoved-host",
			jsonData: `{"comment":{"__name":"dummy-912!f653e951-2210-432d-bca6-e3719ea74ca3","author":"icingaadmin","entry_time":1697191791.097852,"entry_type":1,"expire_time":0,"host_name":"dummy-912","legacy_id":1,"name":"f653e951-2210-432d-bca6-e3719ea74ca3","package":"_api","persistent":false,"service_name":"","source_location":{"first_column":0,"first_line":1,"last_column":68,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/comments/dummy-912!f653e951-2210-432d-bca6-e3719ea74ca3.conf"},"sticky":false,"templates":["f653e951-2210-432d-bca6-e3719ea74ca3"],"text":"oh noes","type":"Comment","version":1697191791.097867,"zone":"master"},"timestamp":1697191807.910093,"type":"CommentRemoved"}`,
			expected: &CommentRemoved{
				Timestamp: Icinga2Time{time.UnixMicro(1697191807910093)},
				Comment: Comment{
					Host:      "dummy-912",
					Author:    "icingaadmin",
					Text:      "oh noes",
					EntryType: 1,
					EntryTime: Icinga2Time{time.UnixMicro(1697191791097852)},
				},
			},
		},
		{
			name:     "commentremoved-service",
			jsonData: `{"comment":{"__name":"dummy-912!ping4!8c00fb6a-5948-4249-a9d5-d1b6eb8945d0","author":"icingaadmin","entry_time":1697197990.035889,"entry_type":1,"expire_time":0,"host_name":"dummy-912","legacy_id":8,"name":"8c00fb6a-5948-4249-a9d5-d1b6eb8945d0","package":"_api","persistent":false,"service_name":"ping4","source_location":{"first_column":0,"first_line":1,"last_column":68,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/comments/dummy-912!ping4!8c00fb6a-5948-4249-a9d5-d1b6eb8945d0.conf"},"sticky":false,"templates":["8c00fb6a-5948-4249-a9d5-d1b6eb8945d0"],"text":"if in doubt, check ticket #23","type":"Comment","version":1697197990.035905,"zone":"master"},"timestamp":1697197996.584392,"type":"CommentRemoved"}`,
			expected: &CommentRemoved{
				Timestamp: Icinga2Time{time.UnixMicro(1697197996584392)},
				Comment: Comment{
					Host:      "dummy-912",
					Service:   "ping4",
					Author:    "icingaadmin",
					Text:      "if in doubt, check ticket #23",
					EntryType: 1,
					EntryTime: Icinga2Time{time.UnixMicro(1697197990035889)},
				},
			},
		},
		{
			name:     "downtimeadded-host",
			jsonData: `{"downtime":{"__name":"dummy-157!e5d4d4ac-615a-4995-ab8f-09d9cd9503b1","author":"icingaadmin","authoritative_zone":"","comment":"updates","config_owner":"","config_owner_hash":"","duration":0,"end_time":1697210639,"entry_time":1697207050.509957,"fixed":true,"host_name":"dummy-157","legacy_id":3,"name":"e5d4d4ac-615a-4995-ab8f-09d9cd9503b1","package":"_api","parent":"","remove_time":0,"scheduled_by":"","service_name":"","source_location":{"first_column":0,"first_line":1,"last_column":69,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/downtimes/dummy-157!e5d4d4ac-615a-4995-ab8f-09d9cd9503b1.conf"},"start_time":1697207039,"templates":["e5d4d4ac-615a-4995-ab8f-09d9cd9503b1"],"trigger_time":0,"triggered_by":"","triggers":[],"type":"Downtime","version":1697207050.509971,"zone":"master"},"timestamp":1697207050.511293,"type":"DowntimeAdded"}`,
			expected: &DowntimeAdded{
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
			expected: &DowntimeAdded{
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
			expected: &DowntimeStarted{
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
			expected: &DowntimeStarted{
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
			expected: &DowntimeTriggered{
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
			expected: &DowntimeTriggered{
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
			expected: &DowntimeRemoved{
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
			expected: &DowntimeRemoved{
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
				t.Errorf("unexpected error state; got error: %t, expected: %t; %v", err != nil, test.isError, err)
				return
			} else if err != nil {
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
