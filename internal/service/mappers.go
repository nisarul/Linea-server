// SPDX-License-Identifier: AGPL-3.0-or-later

package service

import (
	"time"

	"github.com/nisarul/Linea-core/model"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/nisarul/Linea-server/gen/go/linea/v1"
)

// ----- enum mappers (proto <-> model) -----

func certaintyToProto(c model.Certainty) pb.Certainty {
	switch c {
	case model.CertaintyCertain:
		return pb.Certainty_CERTAINTY_CERTAIN
	case model.CertaintyProbable:
		return pb.Certainty_CERTAINTY_PROBABLE
	case model.CertaintyUncertain:
		return pb.Certainty_CERTAINTY_UNCERTAIN
	}
	return pb.Certainty_CERTAINTY_UNSPECIFIED
}

func certaintyFromProto(c pb.Certainty) model.Certainty {
	switch c {
	case pb.Certainty_CERTAINTY_CERTAIN:
		return model.CertaintyCertain
	case pb.Certainty_CERTAINTY_PROBABLE:
		return model.CertaintyProbable
	case pb.Certainty_CERTAINTY_UNCERTAIN:
		return model.CertaintyUncertain
	}
	return 0
}

func continuityToProto(c model.Continuity) *pb.Continuity {
	out := &pb.Continuity{}
	switch c.State {
	case model.ContinuityContinuous:
		out.State = pb.ContinuityState_CONTINUITY_STATE_CONTINUOUS
	case model.ContinuityGapped:
		out.State = pb.ContinuityState_CONTINUITY_STATE_GAPPED
		out.GapKnownSize = c.Gap.KnownSize
		if c.Gap.KnownSize {
			out.GapSize = int32(c.Gap.Size)
		}
	}
	return out
}

func continuityFromProto(c *pb.Continuity) (model.Continuity, error) {
	if c == nil {
		return model.NewContinuous(), nil
	}
	switch c.GetState() {
	case pb.ContinuityState_CONTINUITY_STATE_CONTINUOUS:
		return model.NewContinuous(), nil
	case pb.ContinuityState_CONTINUITY_STATE_GAPPED:
		if c.GetGapKnownSize() {
			gg, err := model.KnownGap(int(c.GetGapSize()))
			if err != nil {
				return model.Continuity{}, err
			}
			return model.NewGapped(gg), nil
		}
		return model.NewGapped(model.UnknownGap()), nil
	}
	return model.NewContinuous(), nil
}

func relTypeToProto(t model.RelationshipType) pb.RelationshipType {
	switch t {
	case model.RelTypeParentChild:
		return pb.RelationshipType_RELATIONSHIP_TYPE_PARENT_CHILD
	case model.RelTypeMarriage:
		return pb.RelationshipType_RELATIONSHIP_TYPE_MARRIAGE
	}
	return pb.RelationshipType_RELATIONSHIP_TYPE_UNSPECIFIED
}

func relTypeFromProto(t pb.RelationshipType) model.RelationshipType {
	switch t {
	case pb.RelationshipType_RELATIONSHIP_TYPE_PARENT_CHILD:
		return model.RelTypeParentChild
	case pb.RelationshipType_RELATIONSHIP_TYPE_MARRIAGE:
		return model.RelTypeMarriage
	}
	return 0
}

func proposalStateToProto(s model.ProposalState) pb.ProposalState {
	switch s {
	case model.ProposalDraft:
		return pb.ProposalState_PROPOSAL_STATE_DRAFT
	case model.ProposalSubmitted:
		return pb.ProposalState_PROPOSAL_STATE_SUBMITTED
	case model.ProposalUnderReview:
		return pb.ProposalState_PROPOSAL_STATE_UNDER_REVIEW
	case model.ProposalAccepted:
		return pb.ProposalState_PROPOSAL_STATE_ACCEPTED
	case model.ProposalRejected:
		return pb.ProposalState_PROPOSAL_STATE_REJECTED
	case model.ProposalWithdrawn:
		return pb.ProposalState_PROPOSAL_STATE_WITHDRAWN
	}
	return pb.ProposalState_PROPOSAL_STATE_UNSPECIFIED
}

func proposalStateFromProto(s pb.ProposalState) model.ProposalState {
	switch s {
	case pb.ProposalState_PROPOSAL_STATE_DRAFT:
		return model.ProposalDraft
	case pb.ProposalState_PROPOSAL_STATE_SUBMITTED:
		return model.ProposalSubmitted
	case pb.ProposalState_PROPOSAL_STATE_UNDER_REVIEW:
		return model.ProposalUnderReview
	case pb.ProposalState_PROPOSAL_STATE_ACCEPTED:
		return model.ProposalAccepted
	case pb.ProposalState_PROPOSAL_STATE_REJECTED:
		return model.ProposalRejected
	case pb.ProposalState_PROPOSAL_STATE_WITHDRAWN:
		return model.ProposalWithdrawn
	}
	return 0
}

func actionFromProto(a pb.ProposalAction) model.ProposalAction {
	switch a {
	case pb.ProposalAction_PROPOSAL_ACTION_CREATE:
		return model.ProposalActionCreate
	case pb.ProposalAction_PROPOSAL_ACTION_UPDATE:
		return model.ProposalActionUpdate
	case pb.ProposalAction_PROPOSAL_ACTION_RETRACT:
		return model.ProposalActionRetract
	case pb.ProposalAction_PROPOSAL_ACTION_MERGE:
		return model.ProposalActionMerge
	case pb.ProposalAction_PROPOSAL_ACTION_SAME_AS_LINK:
		return model.ProposalActionSameAsLink
	}
	return 0
}

func entityKindFromProto(k pb.EntityKind) model.EntityKind {
	switch k {
	case pb.EntityKind_ENTITY_KIND_PERSON:
		return model.EntityKindPerson
	case pb.EntityKind_ENTITY_KIND_RELATIONSHIP:
		return model.EntityKindRelationship
	case pb.EntityKind_ENTITY_KIND_SOURCE:
		return model.EntityKindSource
	}
	return 0
}

func actionToProto(a model.ProposalAction) pb.ProposalAction {
	switch a {
	case model.ProposalActionCreate:
		return pb.ProposalAction_PROPOSAL_ACTION_CREATE
	case model.ProposalActionUpdate:
		return pb.ProposalAction_PROPOSAL_ACTION_UPDATE
	case model.ProposalActionRetract:
		return pb.ProposalAction_PROPOSAL_ACTION_RETRACT
	case model.ProposalActionMerge:
		return pb.ProposalAction_PROPOSAL_ACTION_MERGE
	case model.ProposalActionSameAsLink:
		return pb.ProposalAction_PROPOSAL_ACTION_SAME_AS_LINK
	}
	return pb.ProposalAction_PROPOSAL_ACTION_UNSPECIFIED
}

func entityKindToProto(k model.EntityKind) pb.EntityKind {
	switch k {
	case model.EntityKindPerson:
		return pb.EntityKind_ENTITY_KIND_PERSON
	case model.EntityKindRelationship:
		return pb.EntityKind_ENTITY_KIND_RELATIONSHIP
	case model.EntityKindSource:
		return pb.EntityKind_ENTITY_KIND_SOURCE
	}
	return pb.EntityKind_ENTITY_KIND_UNSPECIFIED
}

func sourceTypeToProto(t model.SourceType) pb.SourceType {
	switch t {
	case model.SourceTypePrimary:
		return pb.SourceType_SOURCE_TYPE_PRIMARY
	case model.SourceTypeSecondary:
		return pb.SourceType_SOURCE_TYPE_SECONDARY
	case model.SourceTypeOral:
		return pb.SourceType_SOURCE_TYPE_ORAL
	case model.SourceTypeDerived:
		return pb.SourceType_SOURCE_TYPE_DERIVED
	case model.SourceTypeOther:
		return pb.SourceType_SOURCE_TYPE_OTHER
	}
	return pb.SourceType_SOURCE_TYPE_UNSPECIFIED
}

// ----- entity mappers -----

func idToProto(id model.ID) *pb.Id { return &pb.Id{Value: id.String()} }

func idFromProto(p *pb.Id) (model.ID, error) {
	if p == nil {
		return "", nil
	}
	return model.ParseID(p.GetValue())
}

func timeRangeToProto(tr model.TimeRange) *pb.TimeRange {
	if tr.IsZero() {
		return nil
	}
	return &pb.TimeRange{
		EarliestKnown: tr.Earliest.KnownYear,
		EarliestYear:  int32(tr.Earliest.Year),
		LatestKnown:   tr.Latest.KnownYear,
		LatestYear:    int32(tr.Latest.Year),
		Calendar:      string(tr.Calendar),
		Circa:         tr.Circa,
	}
}

func nameToProto(n model.Name) *pb.Name {
	return &pb.Name{
		Text:      n.Text,
		Language:  n.Language,
		Script:    n.Script,
		Type:      string(n.Type),
		Preferred: n.Preferred,
	}
}

func personToProto(p model.Person) *pb.Person {
	out := &pb.Person{
		Id:               idToProto(p.ID()),
		UnknownAncestor:  p.IsUnknownAncestor(),
		Notes:            p.Notes(),
		Gender:           string(p.Gender()),
	}
	for _, n := range p.Names() {
		out.Names = append(out.Names, nameToProto(n))
	}
	if !p.Birth().IsZero() {
		out.Birth = timeRangeToProto(p.Birth())
	}
	if !p.Death().IsZero() {
		out.Death = timeRangeToProto(p.Death())
	}
	return out
}

func relationshipToProto(r model.Relationship) *pb.Relationship {
	out := &pb.Relationship{
		Id:         idToProto(r.ID()),
		FromPerson: idToProto(r.From()),
		ToPerson:   idToProto(r.To()),
		Type:       relTypeToProto(r.Type()),
		Certainty:  certaintyToProto(r.Certainty()),
		Continuity: continuityToProto(r.Continuity()),
		Notes:      r.Notes(),
	}
	if !r.TimeRange().IsZero() {
		out.TimeRange = timeRangeToProto(r.TimeRange())
	}
	for _, sid := range r.Sources() {
		out.Sources = append(out.Sources, idToProto(sid))
	}
	return out
}

func sourceToProto(s model.Source) *pb.Source {
	return &pb.Source{
		Id:       idToProto(s.ID()),
		Type:     sourceTypeToProto(s.Type()),
		Citation: s.Citation(),
		Author:   s.Author(),
		Title:    s.Title(),
		Date:     s.Date(),
		Locator:  s.Locator(),
		Notes:    s.Notes(),
	}
}

func proposalToProto(p model.Proposal) *pb.Proposal {
	out := &pb.Proposal{
		Id:          idToProto(p.ID()),
		State:       proposalStateToProto(p.State()),
		Action:      actionToProto(p.Action()),
		EntityKind:  entityKindToProto(p.EntityKind()),
		TargetId:    idToProto(p.TargetID()),
		SecondaryId: idToProto(p.SecondaryID()),
		Payload:     p.Payload(),
		Reason:      p.Reason(),
		Author:      p.Author(),
	}
	if p.CreatedAt() > 0 {
		out.CreatedAt = timestamppb.New(unixToTime(p.CreatedAt()))
	}
	for _, sid := range p.Sources() {
		out.Sources = append(out.Sources, idToProto(sid))
	}
	for _, h := range p.History() {
		t := &pb.ProposalTransition{
			From:   proposalStateToProto(h.From),
			To:     proposalStateToProto(h.To),
			Actor:  h.Actor,
			Reason: h.Reason,
		}
		if h.Timestamp > 0 {
			t.At = timestamppb.New(unixToTime(h.Timestamp))
		}
		out.History = append(out.History, t)
	}
	return out
}

func unixToTime(s int64) time.Time { return time.Unix(s, 0) }
