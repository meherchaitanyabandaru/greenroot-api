// Who may see and change a quotation.
package quotations

import (
	"context"
	"time"
)

// canBuyerAct verifies the actor is the buyer on this quotation.
// Matches both linked accounts (customer_user_id) and unlinked mobile-only quotations.
// Also enforces valid_until expiry.
func (s *Service) canBuyerAct(ctx context.Context, actor ActorContext, q Quotation) error {
	if actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN") {
		return nil
	}
	// Expiry check: buyer cannot act on an expired quotation.
	if q.ValidUntil != nil && time.Now().After(*q.ValidUntil) {
		return ErrQuotationExpired
	}
	if q.CustomerUserID != nil && *q.CustomerUserID == actor.UserID {
		return nil
	}
	// Allow match by recipient_mobile when customer_user_id is not yet set.
	if q.RecipientMobile != nil && *q.RecipientMobile != "" {
		mobile, err := s.repository.GetUserMobile(ctx, actor.UserID)
		if err == nil && mobile == *q.RecipientMobile {
			return nil
		}
	}
	if q.BuyerNurseryID != nil {
		ownedNurseryID, _ := s.repository.GetOwnedNurseryID(ctx, actor.UserID)
		if ownedNurseryID != nil && *ownedNurseryID == *q.BuyerNurseryID {
			return nil
		}
	}
	return ErrForbidden
}

// canView checks whether the actor may read a quotation.
// Uses the passed context so request cancellation/timeouts are respected.
func (s *Service) canView(ctx context.Context, actor ActorContext, q Quotation) error {
	if actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN") {
		return nil
	}
	if q.CreatedByUserID == actor.UserID {
		return nil
	}
	if q.AssignedManagerUserID != nil && *q.AssignedManagerUserID == actor.UserID {
		return nil
	}
	if q.NurseryID != nil {
		if owner, err := s.repository.IsNurseryOwner(ctx, *q.NurseryID, actor.UserID); err == nil && owner {
			return nil
		}
	}
	if q.QuotationType == "CUSTOMER" && q.Status == "CUSTOMER_DRAFT" {
		return ErrForbidden
	}
	if q.CustomerUserID != nil && *q.CustomerUserID == actor.UserID {
		return nil
	}
	// Allow buyers matched by mobile (quotations where customer_user_id is not yet set
	// but recipient_mobile matches — consistent with the buyer list scope).
	if q.RecipientMobile != nil && *q.RecipientMobile != "" {
		mobile, err := s.repository.GetUserMobile(ctx, actor.UserID)
		if err == nil && mobile == *q.RecipientMobile {
			return nil
		}
	}
	// Allow access when the actor's nursery is the designated buyer nursery.
	if q.BuyerNurseryID != nil {
		ownedNurseryID, _ := s.repository.GetOwnedNurseryID(ctx, actor.UserID)
		if ownedNurseryID != nil && *ownedNurseryID == *q.BuyerNurseryID {
			return nil
		}
	}
	if q.NurseryID != nil {
		// Manager-only actors: already checked creator and assignee above.
		// Don't grant access to any other nursery member — they must be explicitly
		// assigned by the owner to see a quotation they didn't create themselves.
		if isManagerOnly(actor) {
			return ErrForbidden
		}
		member, err := s.repository.IsNurseryMember(ctx, *q.NurseryID, actor.UserID)
		if err != nil {
			return ErrForbidden
		}
		if member {
			return nil
		}
	}
	return ErrForbidden
}

// canManage checks if the actor may approve, recall, convert, or otherwise mutate a quotation's state.
// Both nursery owners and managers qualify; admins always qualify.
func (s *Service) canManage(ctx context.Context, actor ActorContext, q Quotation) error {
	if actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN") {
		return nil
	}
	if q.NurseryID != nil {
		owner, err := s.repository.IsNurseryOwner(ctx, *q.NurseryID, actor.UserID)
		if err == nil && owner {
			return nil
		}
		member, err := s.repository.IsNurseryMember(ctx, *q.NurseryID, actor.UserID)
		if err == nil && member {
			return nil
		}
	}
	return ErrForbidden
}

// isNurseryOwner returns true if actor owns the nursery associated with the quotation.
func (s *Service) isNurseryOwner(ctx context.Context, actor ActorContext, q Quotation) bool {
	if q.NurseryID == nil {
		return false
	}
	owner, err := s.repository.IsNurseryOwner(ctx, *q.NurseryID, actor.UserID)
	return err == nil && owner
}

func (s *Service) scopeList(ctx context.Context, actor ActorContext, input *ListQuotationsRequest) error {
	if actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN") {
		return nil
	}
	if actor.HasRole("DRIVER") {
		return ErrForbidden
	}
	if input.Buying {
		// Buyer perspective: filter by buyer user or buyer nursery.
		input.UserID = actor.UserID
		if actor.HasRole("NURSERY_OWNER") {
			nurseryID, _ := s.repository.GetOwnedNurseryID(ctx, actor.UserID)
			if nurseryID != nil {
				input.BuyerNurseryID = *nurseryID
			}
		}
		return nil
	}
	// Seller perspective: always force scope to the actor's own nursery.
	// Never trust a client-supplied nursery_id — an owner could read another nursery by passing ?nursery_id=X.
	if actor.HasRole("NURSERY_OWNER") || actor.HasRole("MANAGER") {
		nurseryID, _ := s.repository.GetOwnedNurseryID(ctx, actor.UserID)
		if nurseryID == nil && actor.HasRole("MANAGER") {
			nurseryID, _ = s.repository.GetManagerNurseryID(ctx, actor.UserID)
		}
		input.UserID = 0
		if nurseryID != nil {
			input.NurseryID = *nurseryID
		} else {
			input.NurseryID = 0
			input.UserID = actor.UserID
		}
		// Manager-only: restrict to quotations they created or are assigned to.
		// Owner (or owner+manager dual-role) always sees the full nursery list.
		if isManagerOnly(actor) {
			input.ManagerScopeUserID = actor.UserID
			input.UnassignedOnly = false // managers never see the "unassigned" owner view
		}
		return nil
	}
	// Default: buyer/customer sees only their own — force buyer scope so
	// buildWhere matches customer_user_id / recipient_mobile, not created_by_user_id.
	input.Buying = true
	input.UserID = actor.UserID
	return nil
}

// isManagerOnly returns true when the actor is a manager but NOT also an owner or admin.
func isManagerOnly(actor ActorContext) bool {
	return actor.HasRole("MANAGER") &&
		!actor.HasRole("NURSERY_OWNER") &&
		!actor.HasRole("ADMIN") &&
		!actor.HasRole("SUPER_ADMIN")
}

// redactCustomerContact removes customer-identifying details for actors who must not see them.
func redactCustomerContact(q *Quotation) {
	q.RecipientName = nil
	q.RecipientMobile = nil
	q.CustomerUserID = nil
	q.RejectionReason = nil
}
