package icinga2

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestUnixFloat_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		jsonData string
		isError  bool
		expected UnixFloat
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
			expected: UnixFloat(time.Date(1970, time.January, 1, 0, 0, 0, 0, time.UTC)),
		},
		{
			name:     "example-time",
			jsonData: "1697207144.746333",
			expected: UnixFloat(time.Date(2023, time.October, 13, 14, 25, 44, 746333000, time.UTC)),
		},
		{
			name:     "example-time-location",
			jsonData: "1697207144.746333",
			expected: UnixFloat(time.Date(2023, time.October, 13, 16, 25, 44, 746333000,
				time.FixedZone("Europe/Berlin summer", 2*60*60))),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var ici2time UnixFloat
			err := json.Unmarshal([]byte(test.jsonData), &ici2time)
			assert.Equal(t, test.isError, err != nil, "unexpected error state; %v", err)
			if err != nil {
				return
			}

			assert.WithinDuration(t, test.expected.Time(), ici2time.Time(), time.Duration(0))
		})
	}
}

func TestObjectQueriesResult_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		jsonData string
		isError  bool
		resp     any
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
			resp:     &ObjectQueriesResult[Comment]{},
			expected: &ObjectQueriesResult[Comment]{
				Name: "dummy-0!f1239b7d-6e13-4031-b7dd-4055fdd2cd80",
				Type: "Comment",
				Attrs: Comment{
					Host:      "dummy-0",
					Author:    "icingaadmin",
					Text:      "foo bar",
					EntryTime: UnixFloat(time.UnixMicro(1697454753536457)),
					EntryType: EntryTypeUser,
				},
			},
		},
		{
			// $ curl -k -s -u root:icinga 'https://localhost:5665/v1/objects/comments' | jq -c '[.results[] | select(.attrs.service_name != "")][0]'
			name:     "comment-service",
			jsonData: `{"attrs":{"__name":"dummy-912!ping6!1b29580d-0a09-4265-ad1f-5e16f462443d","active":true,"author":"icingaadmin","entry_time":1697197701.307516,"entry_type":1,"expire_time":0,"ha_mode":0,"host_name":"dummy-912","legacy_id":1,"name":"1b29580d-0a09-4265-ad1f-5e16f462443d","original_attributes":null,"package":"_api","paused":false,"persistent":false,"service_name":"ping6","source_location":{"first_column":0,"first_line":1,"last_column":68,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/comments/dummy-912!ping6!1b29580d-0a09-4265-ad1f-5e16f462443d.conf"},"templates":["1b29580d-0a09-4265-ad1f-5e16f462443d"],"text":"adfadsfasdfasdf","type":"Comment","version":1697197701.307536,"zone":"master"},"joins":{},"meta":{},"name":"dummy-912!ping6!1b29580d-0a09-4265-ad1f-5e16f462443d","type":"Comment"}`,
			resp:     &ObjectQueriesResult[Comment]{},
			expected: &ObjectQueriesResult[Comment]{
				Name: "dummy-912!ping6!1b29580d-0a09-4265-ad1f-5e16f462443d",
				Type: "Comment",
				Attrs: Comment{
					Host:      "dummy-912",
					Service:   "ping6",
					Author:    "icingaadmin",
					Text:      "adfadsfasdfasdf",
					EntryType: EntryTypeUser,
					EntryTime: UnixFloat(time.UnixMicro(1697197701307516)),
				},
			},
		},
		{
			// $ curl -k -s -u root:icinga 'https://localhost:5665/v1/objects/downtimes' | jq -c '[.results[] | select(.attrs.service_name == "")][0]'
			name:     "downtime-host",
			jsonData: `{"attrs":{"__name":"dummy-11!af73f9d9-2ed8-45f8-b541-cce3f3fe0f6c","active":true,"author":"icingaadmin","authoritative_zone":"","comment":"turn down for what","config_owner":"","config_owner_hash":"","duration":0,"end_time":1698096240,"entry_time":1697456415.667442,"fixed":true,"ha_mode":0,"host_name":"dummy-11","legacy_id":2,"name":"af73f9d9-2ed8-45f8-b541-cce3f3fe0f6c","original_attributes":null,"package":"_api","parent":"","paused":false,"remove_time":0,"scheduled_by":"","service_name":"","source_location":{"first_column":0,"first_line":1,"last_column":69,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/downtimes/dummy-11!af73f9d9-2ed8-45f8-b541-cce3f3fe0f6c.conf"},"start_time":1697456292,"templates":["af73f9d9-2ed8-45f8-b541-cce3f3fe0f6c"],"trigger_time":1697456415.667442,"triggered_by":"","triggers":[],"type":"Downtime","version":1697456415.667458,"was_cancelled":false,"zone":"master"},"joins":{},"meta":{},"name":"dummy-11!af73f9d9-2ed8-45f8-b541-cce3f3fe0f6c","type":"Downtime"}`,
			resp:     &ObjectQueriesResult[Downtime]{},
			expected: &ObjectQueriesResult[Downtime]{
				Name: "dummy-11!af73f9d9-2ed8-45f8-b541-cce3f3fe0f6c",
				Type: "Downtime",
				Attrs: Downtime{
					Host:       "dummy-11",
					Author:     "icingaadmin",
					Comment:    "turn down for what",
					RemoveTime: UnixFloat(time.UnixMilli(0)),
					IsFixed:    true,
				},
			},
		},
		{
			// $ curl -k -s -u root:icinga 'https://localhost:5665/v1/objects/downtimes' | jq -c '[.results[] | select(.attrs.fixed == false)][1]'
			name:     "flexible-downtime-host",
			jsonData: `{"attrs":{"__name":"dummy-7!691d508b-c93f-4565-819c-3e46ffef1555","active":true,"author":"icingaadmin","authoritative_zone":"","comment":"Flexible","config_owner":"","config_owner_hash":"","duration":7200,"end_time":1714043658,"entry_time":1714040073.241627,"fixed":false,"ha_mode":0,"host_name":"dummy-7","legacy_id":4,"name":"691d508b-c93f-4565-819c-3e46ffef1555","original_attributes":null,"package":"_api","parent":"","paused":false,"remove_time":0,"scheduled_by":"","service_name":"","source_location":{"first_column":0,"first_line":1,"last_column":69,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/e5ec468f-6d29-4055-9cd4-495dbbef16e3/conf.d/downtimes/dummy-7!691d508b-c93f-4565-819c-3e46ffef1555.conf"},"start_time":1714040058,"templates":["691d508b-c93f-4565-819c-3e46ffef1555"],"trigger_time":1714040073.241627,"triggered_by":"","triggers":[],"type":"Downtime","version":1714040073.241642,"was_cancelled":false,"zone":"master"},"joins":{},"meta":{},"name":"dummy-7!691d508b-c93f-4565-819c-3e46ffef1555","type":"Downtime"}`,
			resp:     &ObjectQueriesResult[Downtime]{},
			expected: &ObjectQueriesResult[Downtime]{
				Name: "dummy-7!691d508b-c93f-4565-819c-3e46ffef1555",
				Type: "Downtime",
				Attrs: Downtime{
					Host:       "dummy-7",
					Author:     "icingaadmin",
					Comment:    "Flexible",
					RemoveTime: UnixFloat(time.UnixMilli(0)),
					IsFixed:    false,
				},
			},
		},
		{
			// $ curl -k -s -u root:icinga 'https://localhost:5665/v1/objects/downtimes' | jq -c '[.results[] | select(.attrs.fixed == false)][0]'
			name:     "flexible-downtime-service",
			jsonData: `{"attrs":{"__name":"docker-master!disk /!97078a44-8902-495a-9f2a-c1f6802bc63d","active":true,"author":"icingaadmin","authoritative_zone":"","comment":"Flexible","config_owner":"","config_owner_hash":"","duration":7200,"end_time":1714042731,"entry_time":1714039143.459298,"fixed":false,"ha_mode":0,"host_name":"docker-master","legacy_id":3,"name":"97078a44-8902-495a-9f2a-c1f6802bc63d","original_attributes":null,"package":"_api","parent":"","paused":false,"remove_time":0,"scheduled_by":"","service_name":"disk /","source_location":{"first_column":0,"first_line":1,"last_column":69,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/e5ec468f-6d29-4055-9cd4-495dbbef16e3/conf.d/downtimes/docker-master!disk %2F!97078a44-8902-495a-9f2a-c1f6802bc63d.conf"},"start_time":1714039131,"templates":["97078a44-8902-495a-9f2a-c1f6802bc63d"],"trigger_time":1714039143.459298,"triggered_by":"","triggers":[],"type":"Downtime","version":1714039143.459324,"was_cancelled":false,"zone":""},"joins":{},"meta":{},"name":"docker-master!disk /!97078a44-8902-495a-9f2a-c1f6802bc63d","type":"Downtime"}`,
			resp:     &ObjectQueriesResult[Downtime]{},
			expected: &ObjectQueriesResult[Downtime]{
				Name: "docker-master!disk /!97078a44-8902-495a-9f2a-c1f6802bc63d",
				Type: "Downtime",
				Attrs: Downtime{
					Host:       "docker-master",
					Service:    "disk /",
					Author:     "icingaadmin",
					Comment:    "Flexible",
					RemoveTime: UnixFloat(time.UnixMilli(0)),
					IsFixed:    false,
				},
			},
		},
		{
			// $ curl -k -s -u root:icinga 'https://localhost:5665/v1/objects/downtimes' | jq -c '[.results[] | select(.attrs.service_name != "")][0]'
			name:     "downtime-service",
			jsonData: `{"attrs":{"__name":"docker-master!load!c27b27c2-e0ab-45ff-8b9b-e95f29851eb0","active":true,"author":"icingaadmin","authoritative_zone":"master","comment":"Scheduled downtime for backup","config_owner":"docker-master!load!backup-downtime","config_owner_hash":"ca9502dc8fa5d29c1cb2686808b5d2ccf3ea4a9c6dc3f3c09bfc54614c03c765","duration":0,"end_time":1697511600,"entry_time":1697439555.095232,"fixed":true,"ha_mode":0,"host_name":"docker-master","legacy_id":1,"name":"c27b27c2-e0ab-45ff-8b9b-e95f29851eb0","original_attributes":null,"package":"_api","parent":"","paused":false,"remove_time":0,"scheduled_by":"docker-master!load!backup-downtime","service_name":"load","source_location":{"first_column":0,"first_line":1,"last_column":69,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/downtimes/docker-master!load!c27b27c2-e0ab-45ff-8b9b-e95f29851eb0.conf"},"start_time":1697508000,"templates":["c27b27c2-e0ab-45ff-8b9b-e95f29851eb0"],"trigger_time":0,"triggered_by":"","triggers":[],"type":"Downtime","version":1697439555.095272,"was_cancelled":false,"zone":""},"joins":{},"meta":{},"name":"docker-master!load!c27b27c2-e0ab-45ff-8b9b-e95f29851eb0","type":"Downtime"}`,
			resp:     &ObjectQueriesResult[Downtime]{},
			expected: &ObjectQueriesResult[Downtime]{
				Name: "docker-master!load!c27b27c2-e0ab-45ff-8b9b-e95f29851eb0",
				Type: "Downtime",
				Attrs: Downtime{
					Host:       "docker-master",
					Service:    "load",
					Author:     "icingaadmin",
					Comment:    "Scheduled downtime for backup",
					RemoveTime: UnixFloat(time.UnixMilli(0)),
					IsFixed:    true,
				},
			},
		},
		{
			// $ curl -k -s -u root:icinga 'https://localhost:5665/v1/objects/hosts' | jq -c '.results[0]'
			name:     "host",
			jsonData: `{"attrs":{"__name":"dummy-244","acknowledgement":0,"acknowledgement_expiry":0,"acknowledgement_last_change":0,"action_url":"","active":true,"address":"127.0.0.1","address6":"::1","check_attempt":1,"check_command":"random fortune","check_interval":300,"check_period":"","check_timeout":null,"command_endpoint":"","display_name":"dummy-244","downtime_depth":0,"enable_active_checks":true,"enable_event_handler":true,"enable_flapping":false,"enable_notifications":true,"enable_passive_checks":true,"enable_perfdata":true,"event_command":"icinga-notifications-host-events","executions":null,"flapping":false,"flapping_current":0,"flapping_ignore_states":null,"flapping_last_change":0,"flapping_threshold":0,"flapping_threshold_high":30,"flapping_threshold_low":25,"force_next_check":false,"force_next_notification":false,"groups":["app-network","department-dev","env-qa","location-rome"],"ha_mode":0,"handled":false,"icon_image":"","icon_image_alt":"","last_check":1697459643.869006,"last_check_result":{"active":true,"check_source":"docker-master","command":["/bin/bash","-c","/usr/games/fortune; exit $0","0"],"execution_end":1697459643.868893,"execution_start":1697459643.863147,"exit_status":0,"output":"If you think last Tuesday was a drag, wait till you see what happens tomorrow!","performance_data":[],"previous_hard_state":99,"schedule_end":1697459643.869006,"schedule_start":1697459643.86287,"scheduling_source":"docker-master","state":0,"ttl":0,"type":"CheckResult","vars_after":{"attempt":1,"reachable":true,"state":0,"state_type":1},"vars_before":{"attempt":1,"reachable":true,"state":0,"state_type":1}},"last_hard_state":0,"last_hard_state_change":1697099900.637215,"last_reachable":true,"last_state":0,"last_state_change":1697099900.637215,"last_state_down":0,"last_state_type":1,"last_state_unreachable":0,"last_state_up":1697459643.868893,"max_check_attempts":3,"name":"dummy-244","next_check":1697459943.019035,"next_update":1697460243.031081,"notes":"","notes_url":"","original_attributes":null,"package":"_etc","paused":false,"previous_state_change":1697099900.637215,"problem":false,"retry_interval":60,"severity":0,"source_location":{"first_column":5,"first_line":2,"last_column":38,"last_line":2,"path":"/etc/icinga2/zones.d/master/03-dummys-hosts.conf"},"state":0,"state_type":1,"templates":["dummy-244","generic-icinga-notifications-host"],"type":"Host","vars":{"app":"network","department":"dev","env":"qa","is_dummy":true,"location":"rome"},"version":0,"volatile":false,"zone":"master"},"joins":{},"meta":{},"name":"dummy-244","type":"Host"}`,
			resp:     &ObjectQueriesResult[HostServiceRuntimeAttributes]{},
			expected: &ObjectQueriesResult[HostServiceRuntimeAttributes]{
				Name: "dummy-244",
				Type: "Host",
				Attrs: HostServiceRuntimeAttributes{
					Name:      "dummy-244",
					Groups:    []string{"app-network", "department-dev", "env-qa", "location-rome"},
					State:     StateHostUp,
					StateType: StateTypeHard,
					LastCheckResult: CheckResult{
						ExitStatus:     0,
						Output:         "If you think last Tuesday was a drag, wait till you see what happens tomorrow!",
						State:          StateHostUp,
						ExecutionStart: UnixFloat(time.UnixMicro(1697459643863147)),
						ExecutionEnd:   UnixFloat(time.UnixMicro(1697459643868893)),
					},
					LastStateChange:           UnixFloat(time.UnixMicro(1697099900637215)),
					DowntimeDepth:             0,
					Acknowledgement:           AcknowledgementNone,
					AcknowledgementLastChange: UnixFloat(time.UnixMilli(0)),
				},
			},
		},
		{
			// $ curl -k -s -u root:icinga -d '{"filter": "service.acknowledgement != 0"}' -H 'Accept: application/json' -H 'X-HTTP-Method-Override: GET' 'https://localhost:5665/v1/objects/services' | jq -c '.results[0]'
			name:     "service",
			jsonData: `{"attrs":{"__name":"docker-master!ssh","acknowledgement":1,"acknowledgement_expiry":0,"acknowledgement_last_change":1697460655.878141,"action_url":"","active":true,"check_attempt":1,"check_command":"ssh","check_interval":60,"check_period":"","check_timeout":null,"command_endpoint":"","display_name":"ssh","downtime_depth":0,"enable_active_checks":true,"enable_event_handler":true,"enable_flapping":false,"enable_notifications":true,"enable_passive_checks":true,"enable_perfdata":true,"event_command":"icinga-notifications-service-events","executions":null,"flapping":false,"flapping_current":0,"flapping_ignore_states":null,"flapping_last_change":0,"flapping_threshold":0,"flapping_threshold_high":30,"flapping_threshold_low":25,"force_next_check":false,"force_next_notification":false,"groups":[],"ha_mode":0,"handled":true,"host_name":"docker-master","icon_image":"","icon_image_alt":"","last_check":1697460711.134904,"last_check_result":{"active":true,"check_source":"docker-master","command":["/usr/lib/nagios/plugins/check_ssh","127.0.0.1"],"execution_end":1697460711.134875,"execution_start":1697460711.130247,"exit_status":2,"output":"connect to address 127.0.0.1 and port 22: Connection refused","performance_data":[],"previous_hard_state":99,"schedule_end":1697460711.134904,"schedule_start":1697460711.13,"scheduling_source":"docker-master","state":2,"ttl":0,"type":"CheckResult","vars_after":{"attempt":1,"reachable":true,"state":2,"state_type":1},"vars_before":{"attempt":1,"reachable":true,"state":2,"state_type":1}},"last_hard_state":2,"last_hard_state_change":1697099980.820806,"last_reachable":true,"last_state":2,"last_state_change":1697099896.120829,"last_state_critical":1697460711.134875,"last_state_ok":0,"last_state_type":1,"last_state_unknown":0,"last_state_unreachable":0,"last_state_warning":0,"max_check_attempts":5,"name":"ssh","next_check":1697460771.1299999,"next_update":1697460831.1397498,"notes":"","notes_url":"","original_attributes":null,"package":"_etc","paused":false,"previous_state_change":1697099896.120829,"problem":true,"retry_interval":30,"severity":640,"source_location":{"first_column":1,"first_line":47,"last_column":19,"last_line":47,"path":"/etc/icinga2/conf.d/services.conf"},"state":2,"state_type":1,"templates":["ssh","generic-icinga-notifications-service","generic-service"],"type":"Service","vars":null,"version":0,"volatile":false,"zone":""},"joins":{},"meta":{},"name":"docker-master!ssh","type":"Service"}`,
			resp:     &ObjectQueriesResult[HostServiceRuntimeAttributes]{},
			expected: &ObjectQueriesResult[HostServiceRuntimeAttributes]{
				Name: "docker-master!ssh",
				Type: "Service",
				Attrs: HostServiceRuntimeAttributes{
					Name:      "ssh",
					Host:      "docker-master",
					Groups:    []string{},
					State:     StateServiceCritical,
					StateType: StateTypeHard,
					LastCheckResult: CheckResult{
						ExitStatus:     2,
						Output:         "connect to address 127.0.0.1 and port 22: Connection refused",
						State:          StateServiceCritical,
						ExecutionStart: UnixFloat(time.UnixMicro(1697460711130247)),
						ExecutionEnd:   UnixFloat(time.UnixMicro(1697460711134875)),
					},
					LastStateChange:           UnixFloat(time.UnixMicro(1697099896120829)),
					DowntimeDepth:             0,
					Acknowledgement:           AcknowledgementNormal,
					AcknowledgementLastChange: UnixFloat(time.UnixMicro(1697460655878141)),
				},
			},
		},
		{
			// $ curl -k -s -u root:icinga 'https://localhost:5665/v1/objects/services' | jq -c '[.results[] | select(.attrs.last_check_result.command|type=="string")][0]'
			name:     "service-single-command",
			jsonData: `{"attrs":{"__name":"docker-master!icinga","acknowledgement":0,"acknowledgement_expiry":0,"acknowledgement_last_change":0,"action_url":"","active":true,"check_attempt":1,"check_command":"icinga","check_interval":60,"check_period":"","check_timeout":null,"command_endpoint":"","display_name":"icinga","downtime_depth":0,"enable_active_checks":true,"enable_event_handler":true,"enable_flapping":false,"enable_notifications":true,"enable_passive_checks":true,"enable_perfdata":true,"event_command":"","executions":null,"flapping":false,"flapping_current":0,"flapping_ignore_states":null,"flapping_last_change":0,"flapping_threshold":0,"flapping_threshold_high":30,"flapping_threshold_low":25,"force_next_check":false,"force_next_notification":false,"groups":[],"ha_mode":0,"handled":false,"host_name":"docker-master","icon_image":"","icon_image_alt":"","last_check":1698673636.071483,"last_check_result":{"active":true,"check_source":"docker-master","command":"icinga","execution_end":1698673636.071483,"execution_start":1698673636.068106,"exit_status":0,"output":"Icinga 2 has been running for 26 seconds. Version: v2.14.0-35-g31b1294ac","performance_data":[{"counter":false,"crit":null,"label":"api_num_conn_endpoints","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"api_num_endpoints","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"api_num_http_clients","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"api_num_json_rpc_anonymous_clients","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"api_num_json_rpc_relay_queue_item_rate","max":null,"min":null,"type":"PerfdataValue","unit":"","value":186.86666666666667,"warn":null},{"counter":false,"crit":null,"label":"api_num_json_rpc_relay_queue_items","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"api_num_json_rpc_sync_queue_item_rate","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"api_num_json_rpc_sync_queue_items","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"api_num_json_rpc_work_queue_item_rate","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"api_num_not_conn_endpoints","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"checkercomponent_checker_idle","max":null,"min":null,"type":"PerfdataValue","unit":"","value":4020,"warn":null},{"counter":false,"crit":null,"label":"checkercomponent_checker_pending","max":null,"min":null,"type":"PerfdataValue","unit":"","value":1,"warn":null},{"counter":false,"crit":null,"label":"idomysqlconnection_ido-mysql_queries_rate","max":null,"min":null,"type":"PerfdataValue","unit":"","value":1526.9166666666667,"warn":null},{"counter":false,"crit":null,"label":"idomysqlconnection_ido-mysql_queries_1min","max":null,"min":null,"type":"PerfdataValue","unit":"","value":91615,"warn":null},{"counter":false,"crit":null,"label":"idomysqlconnection_ido-mysql_queries_5mins","max":null,"min":null,"type":"PerfdataValue","unit":"","value":91615,"warn":null},{"counter":false,"crit":null,"label":"idomysqlconnection_ido-mysql_queries_15mins","max":null,"min":null,"type":"PerfdataValue","unit":"","value":91615,"warn":null},{"counter":false,"crit":null,"label":"idomysqlconnection_ido-mysql_query_queue_items","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"idomysqlconnection_ido-mysql_query_queue_item_rate","max":null,"min":null,"type":"PerfdataValue","unit":"","value":381.5833333333333,"warn":null},{"counter":false,"crit":null,"label":"idopgsqlconnection_ido-pgsql_queries_rate","max":null,"min":null,"type":"PerfdataValue","unit":"","value":1527.15,"warn":null},{"counter":false,"crit":null,"label":"idopgsqlconnection_ido-pgsql_queries_1min","max":null,"min":null,"type":"PerfdataValue","unit":"","value":91629,"warn":null},{"counter":false,"crit":null,"label":"idopgsqlconnection_ido-pgsql_queries_5mins","max":null,"min":null,"type":"PerfdataValue","unit":"","value":91629,"warn":null},{"counter":false,"crit":null,"label":"idopgsqlconnection_ido-pgsql_queries_15mins","max":null,"min":null,"type":"PerfdataValue","unit":"","value":91629,"warn":null},{"counter":false,"crit":null,"label":"idopgsqlconnection_ido-pgsql_query_queue_items","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"idopgsqlconnection_ido-pgsql_query_queue_item_rate","max":null,"min":null,"type":"PerfdataValue","unit":"","value":381.56666666666666,"warn":null},{"counter":false,"crit":null,"label":"active_host_checks","max":null,"min":null,"type":"PerfdataValue","unit":"","value":16.286730297242745,"warn":null},{"counter":false,"crit":null,"label":"passive_host_checks","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"active_host_checks_1min","max":null,"min":null,"type":"PerfdataValue","unit":"","value":451,"warn":null},{"counter":false,"crit":null,"label":"passive_host_checks_1min","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"active_host_checks_5min","max":null,"min":null,"type":"PerfdataValue","unit":"","value":451,"warn":null},{"counter":false,"crit":null,"label":"passive_host_checks_5min","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"active_host_checks_15min","max":null,"min":null,"type":"PerfdataValue","unit":"","value":451,"warn":null},{"counter":false,"crit":null,"label":"passive_host_checks_15min","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"active_service_checks","max":null,"min":null,"type":"PerfdataValue","unit":"","value":47.34161464023706,"warn":null},{"counter":false,"crit":null,"label":"passive_service_checks","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"active_service_checks_1min","max":null,"min":null,"type":"PerfdataValue","unit":"","value":1295,"warn":null},{"counter":false,"crit":null,"label":"passive_service_checks_1min","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"active_service_checks_5min","max":null,"min":null,"type":"PerfdataValue","unit":"","value":1295,"warn":null},{"counter":false,"crit":null,"label":"passive_service_checks_5min","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"active_service_checks_15min","max":null,"min":null,"type":"PerfdataValue","unit":"","value":1295,"warn":null},{"counter":false,"crit":null,"label":"passive_service_checks_15min","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"current_pending_callbacks","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"current_concurrent_checks","max":null,"min":null,"type":"PerfdataValue","unit":"","value":68,"warn":null},{"counter":false,"crit":null,"label":"remote_check_queue","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"min_latency","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0.00010800361633300781,"warn":null},{"counter":false,"crit":null,"label":"max_latency","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0.003133535385131836,"warn":null},{"counter":false,"crit":null,"label":"avg_latency","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0.0004072719851866463,"warn":null},{"counter":false,"crit":null,"label":"min_execution_time","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0.0009090900421142578,"warn":null},{"counter":false,"crit":null,"label":"max_execution_time","max":null,"min":null,"type":"PerfdataValue","unit":"","value":4.142040014266968,"warn":null},{"counter":false,"crit":null,"label":"avg_execution_time","max":null,"min":null,"type":"PerfdataValue","unit":"","value":1.3660419934632761,"warn":null},{"counter":false,"crit":null,"label":"num_services_ok","max":null,"min":null,"type":"PerfdataValue","unit":"","value":1972,"warn":null},{"counter":false,"crit":null,"label":"num_services_warning","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"num_services_critical","max":null,"min":null,"type":"PerfdataValue","unit":"","value":47,"warn":null},{"counter":false,"crit":null,"label":"num_services_unknown","max":null,"min":null,"type":"PerfdataValue","unit":"","value":1001,"warn":null},{"counter":false,"crit":null,"label":"num_services_pending","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"num_services_unreachable","max":null,"min":null,"type":"PerfdataValue","unit":"","value":138,"warn":null},{"counter":false,"crit":null,"label":"num_services_flapping","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"num_services_in_downtime","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"num_services_acknowledged","max":null,"min":null,"type":"PerfdataValue","unit":"","value":2,"warn":null},{"counter":false,"crit":null,"label":"num_services_handled","max":null,"min":null,"type":"PerfdataValue","unit":"","value":149,"warn":null},{"counter":false,"crit":null,"label":"num_services_problem","max":null,"min":null,"type":"PerfdataValue","unit":"","value":1048,"warn":null},{"counter":false,"crit":null,"label":"uptime","max":null,"min":null,"type":"PerfdataValue","unit":"","value":26.343533039093018,"warn":null},{"counter":false,"crit":null,"label":"num_hosts_up","max":null,"min":null,"type":"PerfdataValue","unit":"","value":952,"warn":null},{"counter":false,"crit":null,"label":"num_hosts_down","max":null,"min":null,"type":"PerfdataValue","unit":"","value":49,"warn":null},{"counter":false,"crit":null,"label":"num_hosts_pending","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"num_hosts_unreachable","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"num_hosts_flapping","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"num_hosts_in_downtime","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"num_hosts_acknowledged","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"num_hosts_handled","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"num_hosts_problem","max":null,"min":null,"type":"PerfdataValue","unit":"","value":49,"warn":null},{"counter":false,"crit":null,"label":"last_messages_sent","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"last_messages_received","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"sum_messages_sent_per_second","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"sum_messages_received_per_second","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"sum_bytes_sent_per_second","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null},{"counter":false,"crit":null,"label":"sum_bytes_received_per_second","max":null,"min":null,"type":"PerfdataValue","unit":"","value":0,"warn":null}],"previous_hard_state":99,"schedule_end":1698673636.071483,"schedule_start":1698673636.0680327,"scheduling_source":"docker-master","state":0,"ttl":0,"type":"CheckResult","vars_after":{"attempt":1,"reachable":true,"state":0,"state_type":1},"vars_before":{"attempt":1,"reachable":true,"state":0,"state_type":1}},"last_hard_state":0,"last_hard_state_change":1697704135.75631,"last_reachable":true,"last_state":0,"last_state_change":1697704135.75631,"last_state_critical":0,"last_state_ok":1698673636.071483,"last_state_type":1,"last_state_unknown":0,"last_state_unreachable":0,"last_state_warning":0,"max_check_attempts":5,"name":"icinga","next_check":1698673695.12149,"next_update":1698673755.1283903,"notes":"","notes_url":"","original_attributes":null,"package":"_etc","paused":false,"previous_state_change":1697704135.75631,"problem":false,"retry_interval":30,"severity":0,"source_location":{"first_column":1,"first_line":73,"last_column":22,"last_line":73,"path":"/etc/icinga2/conf.d/services.conf"},"state":0,"state_type":1,"templates":["icinga","generic-service"],"type":"Service","vars":null,"version":0,"volatile":false,"zone":""},"joins":{},"meta":{},"name":"docker-master!icinga","type":"Service"}`,
			resp:     &ObjectQueriesResult[HostServiceRuntimeAttributes]{},
			expected: &ObjectQueriesResult[HostServiceRuntimeAttributes]{
				Name: "docker-master!icinga",
				Type: "Service",
				Attrs: HostServiceRuntimeAttributes{
					Name:      "icinga",
					Host:      "docker-master",
					Groups:    []string{},
					State:     StateServiceOk,
					StateType: StateTypeHard,
					LastCheckResult: CheckResult{
						ExitStatus:     0,
						Output:         "Icinga 2 has been running for 26 seconds. Version: v2.14.0-35-g31b1294ac",
						State:          StateServiceOk,
						ExecutionStart: UnixFloat(time.UnixMicro(1698673636068106)),
						ExecutionEnd:   UnixFloat(time.UnixMicro(1698673636071483)),
					},
					LastStateChange:           UnixFloat(time.UnixMicro(1697704135756310)),
					DowntimeDepth:             0,
					Acknowledgement:           AcknowledgementNone,
					AcknowledgementLastChange: UnixFloat(time.UnixMilli(0)),
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := json.Unmarshal([]byte(test.jsonData), test.resp)
			assert.Equal(t, test.isError, err != nil, "unexpected error state; %v", err)
			if err != nil {
				return
			}

			assert.EqualValuesf(t, test.expected, test.resp, "unexpected ObjectQueriesResult")
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
				Timestamp: UnixFloat(time.UnixMicro(1697188278203504)),
				Host:      "dummy-158",
				State:     StateHostDown,
				StateType: StateTypeSoft,
				CheckResult: CheckResult{
					ExitStatus: 2,
					Output:     "If two people love each other, there can be no happy end to it.\n\t\t-- Ernest Hemingway",
					// The State will be mapped to StateHostDown within Icinga 2, as shown in the outer StateChange
					// State field. https://github.com/Icinga/icinga2/blob/v2.14.1/lib/icinga/host.cpp#L141-L155
					State:          StateServiceCritical,
					ExecutionStart: UnixFloat(time.UnixMicro(1697188278194409)),
					ExecutionEnd:   UnixFloat(time.UnixMicro(1697188278202986)),
				},
				DowntimeDepth:   0,
				Acknowledgement: false,
			},
		},
		{
			name:     "statechange-service-valid",
			jsonData: `{"acknowledgement":false,"check_result":{"active":true,"check_source":"docker-master","command":["/bin/bash","-c","/usr/games/fortune; exit $0","2"],"execution_end":1697184778.611465,"execution_start":1697184778.600973,"exit_status":2,"output":"You're growing out of some of your problems, but there are others that\nyou're growing into.","performance_data":[],"previous_hard_state":0,"schedule_end":1697184778.611557,"schedule_start":1697184778.6,"scheduling_source":"docker-master","state":2,"ttl":0,"type":"CheckResult","vars_after":{"attempt":2,"reachable":false,"state":2,"state_type":0},"vars_before":{"attempt":1,"reachable":false,"state":2,"state_type":0}},"downtime_depth":0,"host":"dummy-280","service":"random fortune","state":2,"state_type":0,"timestamp":1697184778.612108,"type":"StateChange"}`,
			expected: &StateChange{
				Timestamp: UnixFloat(time.UnixMicro(1697184778612108)),
				Host:      "dummy-280",
				Service:   "random fortune",
				State:     StateServiceCritical,
				StateType: StateTypeSoft,
				CheckResult: CheckResult{
					ExitStatus:     2,
					Output:         "You're growing out of some of your problems, but there are others that\nyou're growing into.",
					State:          StateServiceCritical,
					ExecutionStart: UnixFloat(time.UnixMicro(1697184778600973)),
					ExecutionEnd:   UnixFloat(time.UnixMicro(1697184778611465)),
				},
				DowntimeDepth:   0,
				Acknowledgement: false,
			},
		},
		{
			name:     "acknowledgementset-host",
			jsonData: `{"acknowledgement_type":1,"author":"icingaadmin","comment":"working on it","expiry":0,"host":"dummy-805","notify":true,"persistent":false,"state":1,"state_type":1,"timestamp":1697201074.579106,"type":"AcknowledgementSet"}`,
			expected: &AcknowledgementSet{
				Timestamp: UnixFloat(time.UnixMicro(1697201074579106)),
				Host:      "dummy-805",
				State:     StateHostDown,
				StateType: StateTypeHard,
				Author:    "icingaadmin",
				Comment:   "working on it",
			},
		},
		{
			name:     "acknowledgementset-service",
			jsonData: `{"acknowledgement_type":1,"author":"icingaadmin","comment":"will be fixed soon","expiry":0,"host":"docker-master","notify":true,"persistent":false,"service":"ssh","state":2,"state_type":1,"timestamp":1697201107.64792,"type":"AcknowledgementSet"}`,
			expected: &AcknowledgementSet{
				Timestamp: UnixFloat(time.UnixMicro(1697201107647920)),
				Host:      "docker-master",
				Service:   "ssh",
				State:     StateServiceCritical,
				StateType: StateTypeHard,
				Author:    "icingaadmin",
				Comment:   "will be fixed soon",
			},
		},
		{
			name:     "acknowledgementcleared-host",
			jsonData: `{"acknowledgement_type":0,"host":"dummy-805","state":1,"state_type":1,"timestamp":1697201082.440148,"type":"AcknowledgementCleared"}`,
			expected: &AcknowledgementCleared{
				Timestamp: UnixFloat(time.UnixMicro(1697201082440148)),
				Host:      "dummy-805",
				State:     StateHostDown,
				StateType: StateTypeHard,
			},
		},
		{
			name:     "acknowledgementcleared-service",
			jsonData: `{"acknowledgement_type":0,"host":"docker-master","service":"ssh","state":2,"state_type":1,"timestamp":1697201110.220349,"type":"AcknowledgementCleared"}`,
			expected: &AcknowledgementCleared{
				Timestamp: UnixFloat(time.UnixMicro(1697201110220349)),
				Host:      "docker-master",
				Service:   "ssh",
				State:     StateServiceCritical,
				StateType: StateTypeHard,
			},
		},
		{
			name:     "commentadded-host",
			jsonData: `{"comment":{"__name":"dummy-912!f653e951-2210-432d-bca6-e3719ea74ca3","author":"icingaadmin","entry_time":1697191791.097852,"entry_type":1,"expire_time":0,"host_name":"dummy-912","legacy_id":1,"name":"f653e951-2210-432d-bca6-e3719ea74ca3","package":"_api","persistent":false,"service_name":"","source_location":{"first_column":0,"first_line":1,"last_column":68,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/comments/dummy-912!f653e951-2210-432d-bca6-e3719ea74ca3.conf"},"sticky":false,"templates":["f653e951-2210-432d-bca6-e3719ea74ca3"],"text":"oh noes","type":"Comment","version":1697191791.097867,"zone":"master"},"timestamp":1697191791.099201,"type":"CommentAdded"}`,
			expected: &CommentAdded{
				Timestamp: UnixFloat(time.UnixMicro(1697191791099201)),
				Comment: Comment{
					Host:      "dummy-912",
					Author:    "icingaadmin",
					Text:      "oh noes",
					EntryType: EntryTypeUser,
					EntryTime: UnixFloat(time.UnixMicro(1697191791097852)),
				},
			},
		},
		{
			name:     "commentadded-service",
			jsonData: `{"comment":{"__name":"dummy-912!ping4!8c00fb6a-5948-4249-a9d5-d1b6eb8945d0","author":"icingaadmin","entry_time":1697197990.035889,"entry_type":1,"expire_time":0,"host_name":"dummy-912","legacy_id":8,"name":"8c00fb6a-5948-4249-a9d5-d1b6eb8945d0","package":"_api","persistent":false,"service_name":"ping4","source_location":{"first_column":0,"first_line":1,"last_column":68,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/comments/dummy-912!ping4!8c00fb6a-5948-4249-a9d5-d1b6eb8945d0.conf"},"sticky":false,"templates":["8c00fb6a-5948-4249-a9d5-d1b6eb8945d0"],"text":"if in doubt, check ticket #23","type":"Comment","version":1697197990.035905,"zone":"master"},"timestamp":1697197990.037244,"type":"CommentAdded"}`,
			expected: &CommentAdded{
				Timestamp: UnixFloat(time.UnixMicro(1697197990037244)),
				Comment: Comment{
					Host:      "dummy-912",
					Service:   "ping4",
					Author:    "icingaadmin",
					Text:      "if in doubt, check ticket #23",
					EntryType: EntryTypeUser,
					EntryTime: UnixFloat(time.UnixMicro(1697197990035889)),
				},
			},
		},
		{
			name:     "commentremoved-host",
			jsonData: `{"comment":{"__name":"dummy-912!f653e951-2210-432d-bca6-e3719ea74ca3","author":"icingaadmin","entry_time":1697191791.097852,"entry_type":1,"expire_time":0,"host_name":"dummy-912","legacy_id":1,"name":"f653e951-2210-432d-bca6-e3719ea74ca3","package":"_api","persistent":false,"service_name":"","source_location":{"first_column":0,"first_line":1,"last_column":68,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/comments/dummy-912!f653e951-2210-432d-bca6-e3719ea74ca3.conf"},"sticky":false,"templates":["f653e951-2210-432d-bca6-e3719ea74ca3"],"text":"oh noes","type":"Comment","version":1697191791.097867,"zone":"master"},"timestamp":1697191807.910093,"type":"CommentRemoved"}`,
			expected: &CommentRemoved{
				Timestamp: UnixFloat(time.UnixMicro(1697191807910093)),
				Comment: Comment{
					Host:      "dummy-912",
					Author:    "icingaadmin",
					Text:      "oh noes",
					EntryType: EntryTypeUser,
					EntryTime: UnixFloat(time.UnixMicro(1697191791097852)),
				},
			},
		},
		{
			name:     "commentremoved-service",
			jsonData: `{"comment":{"__name":"dummy-912!ping4!8c00fb6a-5948-4249-a9d5-d1b6eb8945d0","author":"icingaadmin","entry_time":1697197990.035889,"entry_type":1,"expire_time":0,"host_name":"dummy-912","legacy_id":8,"name":"8c00fb6a-5948-4249-a9d5-d1b6eb8945d0","package":"_api","persistent":false,"service_name":"ping4","source_location":{"first_column":0,"first_line":1,"last_column":68,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/comments/dummy-912!ping4!8c00fb6a-5948-4249-a9d5-d1b6eb8945d0.conf"},"sticky":false,"templates":["8c00fb6a-5948-4249-a9d5-d1b6eb8945d0"],"text":"if in doubt, check ticket #23","type":"Comment","version":1697197990.035905,"zone":"master"},"timestamp":1697197996.584392,"type":"CommentRemoved"}`,
			expected: &CommentRemoved{
				Timestamp: UnixFloat(time.UnixMicro(1697197996584392)),
				Comment: Comment{
					Host:      "dummy-912",
					Service:   "ping4",
					Author:    "icingaadmin",
					Text:      "if in doubt, check ticket #23",
					EntryType: EntryTypeUser,
					EntryTime: UnixFloat(time.UnixMicro(1697197990035889)),
				},
			},
		},
		{
			name:     "downtimeadded-host",
			jsonData: `{"downtime":{"__name":"dummy-157!e5d4d4ac-615a-4995-ab8f-09d9cd9503b1","author":"icingaadmin","authoritative_zone":"","comment":"updates","config_owner":"","config_owner_hash":"","duration":0,"end_time":1697210639,"entry_time":1697207050.509957,"fixed":true,"host_name":"dummy-157","legacy_id":3,"name":"e5d4d4ac-615a-4995-ab8f-09d9cd9503b1","package":"_api","parent":"","remove_time":0,"scheduled_by":"","service_name":"","source_location":{"first_column":0,"first_line":1,"last_column":69,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/downtimes/dummy-157!e5d4d4ac-615a-4995-ab8f-09d9cd9503b1.conf"},"start_time":1697207039,"templates":["e5d4d4ac-615a-4995-ab8f-09d9cd9503b1"],"trigger_time":0,"triggered_by":"","triggers":[],"type":"Downtime","version":1697207050.509971,"zone":"master"},"timestamp":1697207050.511293,"type":"DowntimeAdded"}`,
			expected: &DowntimeAdded{
				Timestamp: UnixFloat(time.UnixMicro(1697207050511293)),
				Downtime: Downtime{
					Host:       "dummy-157",
					Author:     "icingaadmin",
					Comment:    "updates",
					RemoveTime: UnixFloat(time.UnixMilli(0)),
					IsFixed:    true,
				},
			},
		},
		{
			name:     "downtimeadded-service",
			jsonData: `{"downtime":{"__name":"docker-master!http!3dabe7e7-32b2-4112-ba8f-a6567e5be79f","author":"icingaadmin","authoritative_zone":"","comment":"broken until Monday","config_owner":"","config_owner_hash":"","duration":0,"end_time":1697210716,"entry_time":1697207141.216009,"fixed":true,"host_name":"docker-master","legacy_id":4,"name":"3dabe7e7-32b2-4112-ba8f-a6567e5be79f","package":"_api","parent":"","remove_time":0,"scheduled_by":"","service_name":"http","source_location":{"first_column":0,"first_line":1,"last_column":69,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/downtimes/docker-master!http!3dabe7e7-32b2-4112-ba8f-a6567e5be79f.conf"},"start_time":1697207116,"templates":["3dabe7e7-32b2-4112-ba8f-a6567e5be79f"],"trigger_time":0,"triggered_by":"","triggers":[],"type":"Downtime","version":1697207141.216025,"zone":""},"timestamp":1697207141.217425,"type":"DowntimeAdded"}`,
			expected: &DowntimeAdded{
				Timestamp: UnixFloat(time.UnixMicro(1697207141217425)),
				Downtime: Downtime{
					Host:       "docker-master",
					Service:    "http",
					Author:     "icingaadmin",
					Comment:    "broken until Monday",
					RemoveTime: UnixFloat(time.UnixMilli(0)),
					IsFixed:    true,
				},
			},
		},
		{
			name:     "downtimestarted-host",
			jsonData: `{"downtime":{"__name":"dummy-157!e5d4d4ac-615a-4995-ab8f-09d9cd9503b1","author":"icingaadmin","authoritative_zone":"","comment":"updates","config_owner":"","config_owner_hash":"","duration":0,"end_time":1697210639,"entry_time":1697207050.509957,"fixed":true,"host_name":"dummy-157","legacy_id":3,"name":"e5d4d4ac-615a-4995-ab8f-09d9cd9503b1","package":"_api","parent":"","remove_time":0,"scheduled_by":"","service_name":"","source_location":{"first_column":0,"first_line":1,"last_column":69,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/downtimes/dummy-157!e5d4d4ac-615a-4995-ab8f-09d9cd9503b1.conf"},"start_time":1697207039,"templates":["e5d4d4ac-615a-4995-ab8f-09d9cd9503b1"],"trigger_time":0,"triggered_by":"","triggers":[],"type":"Downtime","version":1697207050.509971,"zone":"master"},"timestamp":1697207050.511378,"type":"DowntimeStarted"}`,
			expected: &DowntimeStarted{
				Timestamp: UnixFloat(time.UnixMicro(1697207050511378)),
				Downtime: Downtime{
					Host:       "dummy-157",
					Author:     "icingaadmin",
					Comment:    "updates",
					RemoveTime: UnixFloat(time.UnixMilli(0)),
					IsFixed:    true,
				},
			},
		},
		{
			name:     "downtimestarted-service",
			jsonData: `{"downtime":{"__name":"docker-master!http!3dabe7e7-32b2-4112-ba8f-a6567e5be79f","author":"icingaadmin","authoritative_zone":"","comment":"broken until Monday","config_owner":"","config_owner_hash":"","duration":0,"end_time":1697210716,"entry_time":1697207141.216009,"fixed":true,"host_name":"docker-master","legacy_id":4,"name":"3dabe7e7-32b2-4112-ba8f-a6567e5be79f","package":"_api","parent":"","remove_time":0,"scheduled_by":"","service_name":"http","source_location":{"first_column":0,"first_line":1,"last_column":69,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/downtimes/docker-master!http!3dabe7e7-32b2-4112-ba8f-a6567e5be79f.conf"},"start_time":1697207116,"templates":["3dabe7e7-32b2-4112-ba8f-a6567e5be79f"],"trigger_time":0,"triggered_by":"","triggers":[],"type":"Downtime","version":1697207141.216025,"zone":""},"timestamp":1697207141.217507,"type":"DowntimeStarted"}`,
			expected: &DowntimeStarted{
				Timestamp: UnixFloat(time.UnixMicro(1697207141217507)),
				Downtime: Downtime{
					Host:       "docker-master",
					Service:    "http",
					Author:     "icingaadmin",
					Comment:    "broken until Monday",
					RemoveTime: UnixFloat(time.UnixMilli(0)),
					IsFixed:    true,
				},
			},
		},
		{
			name:     "downtimetriggered-host",
			jsonData: `{"downtime":{"__name":"dummy-157!e5d4d4ac-615a-4995-ab8f-09d9cd9503b1","author":"icingaadmin","authoritative_zone":"","comment":"updates","config_owner":"","config_owner_hash":"","duration":0,"end_time":1697210639,"entry_time":1697207050.509957,"fixed":true,"host_name":"dummy-157","legacy_id":3,"name":"e5d4d4ac-615a-4995-ab8f-09d9cd9503b1","package":"_api","parent":"","remove_time":0,"scheduled_by":"","service_name":"","source_location":{"first_column":0,"first_line":1,"last_column":69,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/downtimes/dummy-157!e5d4d4ac-615a-4995-ab8f-09d9cd9503b1.conf"},"start_time":1697207039,"templates":["e5d4d4ac-615a-4995-ab8f-09d9cd9503b1"],"trigger_time":1697207050.509957,"triggered_by":"","triggers":[],"type":"Downtime","version":1697207050.509971,"zone":"master"},"timestamp":1697207050.511608,"type":"DowntimeTriggered"}`,
			expected: &DowntimeTriggered{
				Timestamp: UnixFloat(time.UnixMicro(1697207050511608)),
				Downtime: Downtime{
					Host:       "dummy-157",
					Author:     "icingaadmin",
					Comment:    "updates",
					RemoveTime: UnixFloat(time.UnixMilli(0)),
					IsFixed:    true,
				},
			},
		},
		{
			name:     "flexible-downtimetriggered-host",
			jsonData: `{"downtime":{"__name":"dummy-7!691d508b-c93f-4565-819c-3e46ffef1555","author":"icingaadmin","authoritative_zone":"","comment":"Flexible","config_owner":"","config_owner_hash":"","duration":7200,"end_time":1714043658,"entry_time":1714040073.241627,"fixed":false,"host_name":"dummy-7","legacy_id":4,"name":"691d508b-c93f-4565-819c-3e46ffef1555","package":"_api","parent":"","remove_time":0,"scheduled_by":"","service_name":"","source_location":{"first_column":0,"first_line":1,"last_column":69,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/e5ec468f-6d29-4055-9cd4-495dbbef16e3/conf.d/downtimes/dummy-7!691d508b-c93f-4565-819c-3e46ffef1555.conf"},"start_time":1714040058,"templates":["691d508b-c93f-4565-819c-3e46ffef1555"],"trigger_time":0,"triggered_by":"","triggers":[],"type":"Downtime","version":1714040073.241642,"zone":"master"},"timestamp":1714040073.242575,"type":"DowntimeAdded"}`,
			expected: &DowntimeTriggered{
				Timestamp: UnixFloat(time.UnixMicro(1714040073242575)),
				Downtime: Downtime{
					Host:       "dummy-7",
					Author:     "icingaadmin",
					Comment:    "Flexible",
					RemoveTime: UnixFloat(time.UnixMilli(0)),
					IsFixed:    false,
				},
			},
		},
		{
			name:     "downtimetriggered-service",
			jsonData: `{"downtime":{"__name":"docker-master!http!3dabe7e7-32b2-4112-ba8f-a6567e5be79f","author":"icingaadmin","authoritative_zone":"","comment":"broken until Monday","config_owner":"","config_owner_hash":"","duration":0,"end_time":1697210716,"entry_time":1697207141.216009,"fixed":true,"host_name":"docker-master","legacy_id":4,"name":"3dabe7e7-32b2-4112-ba8f-a6567e5be79f","package":"_api","parent":"","remove_time":0,"scheduled_by":"","service_name":"http","source_location":{"first_column":0,"first_line":1,"last_column":69,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/downtimes/docker-master!http!3dabe7e7-32b2-4112-ba8f-a6567e5be79f.conf"},"start_time":1697207116,"templates":["3dabe7e7-32b2-4112-ba8f-a6567e5be79f"],"trigger_time":1697207141.216009,"triggered_by":"","triggers":[],"type":"Downtime","version":1697207141.216025,"zone":""},"timestamp":1697207141.217726,"type":"DowntimeTriggered"}`,
			expected: &DowntimeTriggered{
				Timestamp: UnixFloat(time.UnixMicro(1697207141217726)),
				Downtime: Downtime{
					Host:       "docker-master",
					Service:    "http",
					Author:     "icingaadmin",
					Comment:    "broken until Monday",
					RemoveTime: UnixFloat(time.UnixMilli(0)),
					IsFixed:    true,
				},
			},
		},
		{
			name:     "flexible-downtimetriggered-service",
			jsonData: `{"downtime":{"__name":"docker-master!disk /!97078a44-8902-495a-9f2a-c1f6802bc63d","author":"icingaadmin","authoritative_zone":"","comment":"Flexible","config_owner":"","config_owner_hash":"","duration":7200,"end_time":1714042731,"entry_time":1714039143.459298,"fixed":false,"host_name":"docker-master","legacy_id":3,"name":"97078a44-8902-495a-9f2a-c1f6802bc63d","package":"_api","parent":"","remove_time":0,"scheduled_by":"","service_name":"disk /","source_location":{"first_column":0,"first_line":1,"last_column":69,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/e5ec468f-6d29-4055-9cd4-495dbbef16e3/conf.d/downtimes/docker-master!disk %2F!97078a44-8902-495a-9f2a-c1f6802bc63d.conf"},"start_time":1714039131,"templates":["97078a44-8902-495a-9f2a-c1f6802bc63d"],"trigger_time":1714039143.459298,"triggered_by":"","triggers":[],"type":"Downtime","version":1714039143.459324,"zone":""},"timestamp":1714039143.460918,"type":"DowntimeTriggered"}`,
			expected: &DowntimeTriggered{
				Timestamp: UnixFloat(time.UnixMicro(1714039143460918)),
				Downtime: Downtime{
					Host:       "docker-master",
					Service:    "disk /",
					Author:     "icingaadmin",
					Comment:    "Flexible",
					RemoveTime: UnixFloat(time.UnixMilli(0)),
					IsFixed:    false,
				},
			},
		},
		{
			name:     "downtimeended-host",
			jsonData: `{"downtime":{"__name":"dummy-157!e5d4d4ac-615a-4995-ab8f-09d9cd9503b1","author":"icingaadmin","authoritative_zone":"","comment":"updates","config_owner":"","config_owner_hash":"","duration":0,"end_time":1697210639,"entry_time":1697207050.509957,"fixed":true,"host_name":"dummy-157","legacy_id":3,"name":"e5d4d4ac-615a-4995-ab8f-09d9cd9503b1","package":"_api","parent":"","remove_time":0.0,"scheduled_by":"","service_name":"","source_location":{"first_column":0,"first_line":1,"last_column":69,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/downtimes/dummy-157!e5d4d4ac-615a-4995-ab8f-09d9cd9503b1.conf"},"start_time":1697207039,"templates":["e5d4d4ac-615a-4995-ab8f-09d9cd9503b1"],"trigger_time":1697207050.509957,"triggered_by":"","triggers":[],"type":"Downtime","version":1697207050.509971,"zone":"master"},"timestamp":1697207096.187866,"type":"DowntimeRemoved"}`,
			expected: &DowntimeRemoved{
				Timestamp: UnixFloat(time.UnixMicro(1697207096187866)),
				Downtime: Downtime{
					Host:       "dummy-157",
					Author:     "icingaadmin",
					Comment:    "updates",
					RemoveTime: UnixFloat(time.UnixMilli(0)),
					IsFixed:    true,
				},
			},
		},
		{
			name:     "downtimeremoved-host",
			jsonData: `{"downtime":{"__name":"dummy-157!e5d4d4ac-615a-4995-ab8f-09d9cd9503b1","author":"icingaadmin","authoritative_zone":"","comment":"updates","config_owner":"","config_owner_hash":"","duration":0,"end_time":1697210639,"entry_time":1697207050.509957,"fixed":true,"host_name":"dummy-157","legacy_id":3,"name":"e5d4d4ac-615a-4995-ab8f-09d9cd9503b1","package":"_api","parent":"","remove_time":1697207096.187718,"scheduled_by":"","service_name":"","source_location":{"first_column":0,"first_line":1,"last_column":69,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/downtimes/dummy-157!e5d4d4ac-615a-4995-ab8f-09d9cd9503b1.conf"},"start_time":1697207039,"templates":["e5d4d4ac-615a-4995-ab8f-09d9cd9503b1"],"trigger_time":1697207050.509957,"triggered_by":"","triggers":[],"type":"Downtime","version":1697207050.509971,"zone":"master"},"timestamp":1697207096.187866,"type":"DowntimeRemoved"}`,
			expected: &DowntimeRemoved{
				Timestamp: UnixFloat(time.UnixMicro(1697207096187866)),
				Downtime: Downtime{
					Host:       "dummy-157",
					Author:     "icingaadmin",
					Comment:    "updates",
					RemoveTime: UnixFloat(time.UnixMicro(1697207096187718)),
					IsFixed:    true,
				},
			},
		},
		{
			name:     "downtimeremoved-service",
			jsonData: `{"downtime":{"__name":"docker-master!http!3dabe7e7-32b2-4112-ba8f-a6567e5be79f","author":"icingaadmin","authoritative_zone":"","comment":"broken until Monday","config_owner":"","config_owner_hash":"","duration":0,"end_time":1697210716,"entry_time":1697207141.216009,"fixed":true,"host_name":"docker-master","legacy_id":4,"name":"3dabe7e7-32b2-4112-ba8f-a6567e5be79f","package":"_api","parent":"","remove_time":1697207144.746117,"scheduled_by":"","service_name":"http","source_location":{"first_column":0,"first_line":1,"last_column":69,"last_line":1,"path":"/var/lib/icinga2/api/packages/_api/997346d3-374d-443f-b734-80789fd59b31/conf.d/downtimes/docker-master!http!3dabe7e7-32b2-4112-ba8f-a6567e5be79f.conf"},"start_time":1697207116,"templates":["3dabe7e7-32b2-4112-ba8f-a6567e5be79f"],"trigger_time":1697207141.216009,"triggered_by":"","triggers":[],"type":"Downtime","version":1697207141.216025,"zone":""},"timestamp":1697207144.746333,"type":"DowntimeRemoved"}`,
			expected: &DowntimeRemoved{
				Timestamp: UnixFloat(time.UnixMicro(1697207144746333)),
				Downtime: Downtime{
					Host:       "docker-master",
					Service:    "http",
					Author:     "icingaadmin",
					Comment:    "broken until Monday",
					RemoveTime: UnixFloat(time.UnixMicro(1697207144746117)),
					IsFixed:    true,
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resp, err := UnmarshalEventStreamResponse([]byte(test.jsonData))
			assert.Equal(t, test.isError, err != nil, "unexpected error state; %v", err)
			if err != nil {
				return
			}

			assert.EqualValuesf(t, test.expected, resp, "unexpected Event Stream response")
		})
	}
}
