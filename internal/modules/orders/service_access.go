// Who may see, create and change an order.
package orders

import (
	"context"
)

func (s *Service) scopeList(ctx context.Context, actor ActorContext, input *ListOrdersRequest) error {
	input.SearchNotes = false
	if actor.HasRole("ADMIN") {
		input.SearchNotes = true
		return nil
	}
	if input.Buying {
		// Buyer perspective: filter by buyer_user_id OR buyer_nursery_id.
		// The buyer nursery is always the caller's own, never the client's
		// nursery_id: buildWhere ORs it in, so a foreign id would list that
		// nursery's purchases.
		input.BuyerID = actor.UserID
		input.NurseryID = 0
		if actor.HasRole("NURSERY_OWNER") {
			nurseryID, _ := s.repository.GetOwnedNurseryID(ctx, actor.UserID)
			if nurseryID != nil {
				input.NurseryID = *nurseryID
			}
		}
		return nil
	}
	if actor.HasRole("NURSERY_OWNER") || actor.HasRole("MANAGER") {
		if input.NurseryID > 0 {
			input.SearchNotes = true
			return s.mustBeNurseryMember(ctx, actor, input.NurseryID)
		}
		nurseryIDs, err := s.repository.GetUserNurseryIDs(ctx, actor.UserID)
		if err != nil {
			return err
		}
		if len(nurseryIDs) == 0 {
			return ErrForbidden
		}
		input.NurseryID = nurseryIDs[0]
		input.SearchNotes = true
		return nil
	}
	if actor.HasRole("DRIVER") {
		return ErrForbidden
	}
	input.BuyerID = actor.UserID
	return nil
}

func (s *Service) canView(ctx context.Context, actor ActorContext, order Order) error {
	if actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN") {
		return nil
	}
	if order.CustomerUserID != nil && *order.CustomerUserID == actor.UserID {
		return nil
	}
	if order.BuyerUserID != nil && *order.BuyerUserID == actor.UserID {
		return nil
	}
	if order.BuyerNurseryID != nil {
		isOwner, _ := s.repository.IsNurseryOwner(ctx, *order.BuyerNurseryID, actor.UserID)
		if isOwner {
			return nil
		}
	}
	nurseryID := order.SellerNurseryID
	if nurseryID != nil {
		owner, _ := s.repository.IsNurseryOwner(ctx, *nurseryID, actor.UserID)
		if owner {
			return nil
		}
		if err := s.mustBeNurseryMember(ctx, actor, *nurseryID); err == nil {
			return nil
		}
	}
	return ErrForbidden
}

func (s *Service) canCreate(ctx context.Context, actor ActorContext, input CreateOrderRequest) error {
	if actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN") {
		return ErrForbidden
	}
	if actor.HasRole("NURSERY_OWNER") || actor.HasRole("MANAGER") {
		if input.SellerNurseryID == nil {
			return ErrInvalidInput
		}
		return s.mustBeNurseryMember(ctx, actor, *input.SellerNurseryID)
	}
	// BUYER creating own order
	if input.BuyerUserID != nil && *input.BuyerUserID != actor.UserID {
		return ErrForbidden
	}
	return nil
}

func (s *Service) canManage(ctx context.Context, actor ActorContext, order Order) error {
	if actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN") {
		return nil
	}
	nurseryID := order.SellerNurseryID
	if nurseryID != nil {
		owner, _ := s.repository.IsNurseryOwner(ctx, *nurseryID, actor.UserID)
		if owner {
			return nil
		}
		if err := s.mustBeNurseryMember(ctx, actor, *nurseryID); err == nil {
			return nil
		}
	}
	return ErrForbidden
}

func (s *Service) mustBeNurseryMember(ctx context.Context, actor ActorContext, nurseryID int64) error {
	member, err := s.repository.IsNurseryMember(ctx, nurseryID, actor.UserID)
	if err != nil {
		return err
	}
	if !member {
		return ErrForbidden
	}
	return nil
}
