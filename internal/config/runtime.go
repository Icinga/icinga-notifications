package config

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/icinga/icingadb/pkg/icingadb"
	"github.com/icinga/icingadb/pkg/logging"
	"github.com/icinga/noma/internal/channel"
	"github.com/icinga/noma/internal/recipient"
	"github.com/icinga/noma/internal/timeperiod"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
	"log"
	"time"
)

// RuntimeConfig stores the runtime representation of the configuration present in the database.
type RuntimeConfig struct {
	Channels        []*channel.Channel
	ChannelByType   map[string]*channel.Channel
	Contacts        []*recipient.Contact
	ContactsByID    map[int64]*recipient.Contact
	Groups          []*recipient.Group
	GroupsByID      map[int64]*recipient.Group
	TimePeriods     []*timeperiod.TimePeriod
	TimePeriodsById map[int64]*timeperiod.TimePeriod
	Schedules       []*recipient.Schedule
	SchedulesByID   map[int64]*recipient.Schedule
}

func (r *RuntimeConfig) UpdateFromDatabase(ctx context.Context, db *icingadb.DB, logger *logging.Logger) error {
	tx, err := db.BeginTxx(ctx, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	if err != nil {
		return err
	}
	// The transaction is only used for reading, never has to be committed.
	defer func() { _ = tx.Rollback() }()

	updateFuncs := []func(ctx context.Context, db *icingadb.DB, tx *sqlx.Tx, logger *logging.Logger) error{
		r.UpdateChannelsFromDatabase,
		r.UpdateContactsFromDatabase,
		r.UpdateGroupsFromDatabase,
		r.UpdateTimePeriodsFromDatabase,
		r.UpdateSchedulesFromDatabase,
	}
	for _, f := range updateFuncs {
		if err := f(ctx, db, tx, logger); err != nil {
			return err
		}
	}

	return nil
}

func (r *RuntimeConfig) UpdateChannelsFromDatabase(ctx context.Context, db *icingadb.DB, tx *sqlx.Tx, logger *logging.Logger) error {
	var channelPtr *channel.Channel
	stmt := db.BuildSelectStmt(channelPtr, channelPtr)
	log.Println(stmt)

	var channels []*channel.Channel
	if err := tx.SelectContext(ctx, &channels, stmt); err != nil {
		log.Println(err)
		return err
	}

	channelsByType := make(map[string]*channel.Channel)
	for _, c := range channels {
		channelLogger := logger.With(
			zap.Int64("id", c.ID),
			zap.String("name", c.Name),
			zap.String("type", c.Type),
		)
		if channelsByType[c.Type] != nil {
			channelLogger.Warnw("ignoring duplicate config for channel type")
		} else {
			channelsByType[c.Type] = c

			channelLogger.Debugw("loaded channel config")
		}
	}

	r.Channels = channels
	r.ChannelByType = channelsByType

	return nil
}

func (r *RuntimeConfig) UpdateContactsFromDatabase(ctx context.Context, db *icingadb.DB, tx *sqlx.Tx, logger *logging.Logger) error {
	var contactPtr *recipient.Contact
	stmt := db.BuildSelectStmt(contactPtr, contactPtr)
	log.Println(stmt)

	var contacts []*recipient.Contact
	if err := tx.SelectContext(ctx, &contacts, stmt); err != nil {
		log.Println(err)
		return err
	}

	contactsByID := make(map[int64]*recipient.Contact)
	for _, c := range contacts {
		contactsByID[c.ID] = c

		logger.Debugw("loaded contact config",
			zap.Int64("id", c.ID),
			zap.String("name", c.FullName))
	}

	var addressPtr *recipient.Address
	stmt = db.BuildSelectStmt(addressPtr, addressPtr)
	log.Println(stmt)

	var addresses []*recipient.Address
	if err := tx.SelectContext(ctx, &addresses, stmt); err != nil {
		log.Println(err)
		return err
	}

	for _, a := range addresses {
		addressLogger := logger.With(
			zap.Int64("contact_id", a.ContactID),
			zap.String("type", a.Type),
			zap.String("address", a.Address),
		)
		if c := contactsByID[a.ContactID]; c != nil {
			c.Addresses = append(c.Addresses, a)

			addressLogger.Debugw("loaded contact address", zap.String("contact_name", c.FullName))
		} else {
			addressLogger.Warnw("ignoring address for unknown contact_id")
		}
	}

	r.Contacts = contacts
	r.ContactsByID = contactsByID

	return nil
}

func (r *RuntimeConfig) UpdateGroupsFromDatabase(ctx context.Context, db *icingadb.DB, tx *sqlx.Tx, logger *logging.Logger) error {
	var groupPtr *recipient.Group
	stmt := db.BuildSelectStmt(groupPtr, groupPtr)
	log.Println(stmt)

	var groups []*recipient.Group
	if err := tx.SelectContext(ctx, &groups, stmt); err != nil {
		log.Println(err)
		return err
	}

	groupsById := make(map[int64]*recipient.Group)
	for _, g := range groups {
		groupsById[g.ID] = g

		logger.Debugw("loaded group config",
			zap.Int64("id", g.ID),
			zap.String("name", g.Name))
	}

	type ContactgroupMember struct {
		GroupId   int64 `db:"contactgroup_id"`
		ContactId int64 `db:"contact_id"`
	}

	var memberPtr *ContactgroupMember
	stmt = db.BuildSelectStmt(memberPtr, memberPtr)
	log.Println(stmt)

	var members []*ContactgroupMember
	if err := tx.SelectContext(ctx, &members, stmt); err != nil {
		log.Println(err)
		return err
	}

	for _, m := range members {
		memberLogger := logger.With(
			zap.Int64("contact_id", m.ContactId),
			zap.Int64("contactgroup_id", m.GroupId),
		)
		if g := groupsById[m.GroupId]; g == nil {
			memberLogger.Warnw("ignoring member for unknown contactgroup_id")
		} else if c := r.ContactsByID[m.ContactId]; c == nil {
			memberLogger.Warnw("ignoring member for unknown contact_id")
		} else {
			g.Members = append(g.Members, c)

			memberLogger.Debugw("loaded contact group member",
				zap.String("contact_name", c.FullName),
				zap.String("contactgroup_name", g.Name))
		}
	}

	r.Groups = groups
	r.GroupsByID = groupsById

	return nil
}

func (r *RuntimeConfig) UpdateTimePeriodsFromDatabase(ctx context.Context, db *icingadb.DB, tx *sqlx.Tx, logger *logging.Logger) error {
	// TODO: At the moment, the timeperiod table contains no interesting fields for the daemon, therefore only
	// entries are fetched and TimePeriod instances are created on the fly.

	type TimeperiodEntry struct {
		ID           int64          `db:"id"`
		TimePeriodID int64          `db:"timeperiod_id"`
		StartTime    int64          `db:"start_time"`
		EndTime      int64          `db:"end_time"`
		Timezone     string         `db:"timezone"`
		RRule        sql.NullString `db:"rrule"`
		Description  sql.NullString `db:"description"`
	}

	var entryPtr *TimeperiodEntry
	stmt := db.BuildSelectStmt(entryPtr, entryPtr)
	log.Println(stmt)

	var entries []*TimeperiodEntry
	if err := tx.SelectContext(ctx, &entries, stmt); err != nil {
		log.Println(err)
		return err
	}

	timePeriodsById := make(map[int64]*timeperiod.TimePeriod)
	for _, row := range entries {
		p := timePeriodsById[row.TimePeriodID]
		if p == nil {
			p = &timeperiod.TimePeriod{
				Name: fmt.Sprintf("Time Period #%d", row.TimePeriodID),
			}
			if row.Description.Valid {
				p.Name += fmt.Sprintf(" (%s)", row.Description.String)
			}
			timePeriodsById[row.TimePeriodID] = p

			logger.Debugw("created time period",
				zap.Int64("id", row.TimePeriodID),
				zap.String("name", p.Name))
		}

		loc, err := time.LoadLocation(row.Timezone)
		if err != nil {
			logger.Warnw("ignoring time period entry with unknown timezone",
				zap.Int64("timeperiod_entry_id", row.ID),
				zap.String("timezone", row.Timezone),
				zap.Error(err))
			continue
		}

		entry := &timeperiod.Entry{
			Start:    time.Unix(row.StartTime, 0).In(loc),
			End:      time.Unix(row.EndTime, 0).In(loc),
			TimeZone: row.Timezone,
		}

		if row.RRule.Valid {
			entry.RecurrenceRule = row.RRule.String
		}

		err = entry.Init()
		if err != nil {
			logger.Warnw("ignoring time period entry",
				zap.Int64("timeperiod_entry_id", row.ID),
				zap.String("rrule", entry.RecurrenceRule),
				zap.Error(err))
			continue
		}

		logger.Debugw("loaded time period entry",
			zap.String("timeperiod", p.Name),
			zap.Time("start", entry.Start),
			zap.Time("end", entry.End),
			zap.String("rrule", entry.RecurrenceRule))
	}

	timePeriods := make([]*timeperiod.TimePeriod, len(timePeriodsById))
	for _, p := range timePeriodsById {
		timePeriods = append(timePeriods, p)
	}

	r.TimePeriods = timePeriods
	r.TimePeriodsById = timePeriodsById

	return nil
}

func (r *RuntimeConfig) UpdateSchedulesFromDatabase(ctx context.Context, db *icingadb.DB, tx *sqlx.Tx, logger *logging.Logger) error {
	var schedulePtr *recipient.Schedule
	stmt := db.BuildSelectStmt(schedulePtr, schedulePtr)
	log.Println(stmt)

	var schedules []*recipient.Schedule
	if err := tx.SelectContext(ctx, &schedules, stmt); err != nil {
		log.Println(err)
		return err
	}

	schedulesById := make(map[int64]*recipient.Schedule)
	for _, g := range schedules {
		schedulesById[g.ID] = g

		logger.Debugw("loaded schedule config",
			zap.Int64("id", g.ID),
			zap.String("name", g.Name))
	}

	type ScheduleMember struct {
		ScheduleID   int64         `db:"schedule_id"`
		TimePeriodID int64         `db:"timeperiod_id"`
		ContactID    sql.NullInt64 `db:"contact_id"`
		GroupID      sql.NullInt64 `db:"contactgroup_id"`
	}

	var memberPtr *ScheduleMember
	stmt = db.BuildSelectStmt(memberPtr, memberPtr)
	log.Println(stmt)

	var members []*ScheduleMember
	if err := tx.SelectContext(ctx, &members, stmt); err != nil {
		log.Println(err)
		return err
	}

	for _, member := range members {
		memberLogger := logger.With(
			zap.Int64("schedule_id", member.ScheduleID),
			zap.Int64("timeperiod_id", member.TimePeriodID),
			zap.Int64("contact_id", member.ContactID.Int64),
			zap.Int64("contactgroup_id", member.GroupID.Int64),
		)

		if s := schedulesById[member.ScheduleID]; s == nil {
			memberLogger.Warnw("ignoring schedule member for unknown schedule_id")
		} else if p := r.TimePeriodsById[member.TimePeriodID]; p == nil {
			memberLogger.Warnw("ignoring schedule member for unknown timeperiod_id")
		} else if c := r.ContactsByID[member.ContactID.Int64]; member.ContactID.Valid && p == nil {
			memberLogger.Warnw("ignoring schedule member for unknown contact_id")
		} else if g := r.GroupsByID[member.GroupID.Int64]; member.GroupID.Valid && p == nil {
			memberLogger.Warnw("ignoring schedule member for unknown contactgroup_id")
		} else {
			s.Members = append(s.Members, &recipient.Member{
				TimePeriod:   p,
				Contact:      c,
				ContactGroup: g,
			})

			memberLogger.Debugw("member")
		}
	}

	r.Schedules = schedules
	r.SchedulesByID = schedulesById

	return nil
}
