package matcher

// watchJaccard is how similar two model-less names' words must be for a
// watch to follow both. Looser than grouping (jaccardAccept): a missed
// listing costs a missed alert, while a stray one is visible in the watch
// editor and can be excluded.
const watchJaccard = 0.75

// SameSuit reports whether other names the same mobile suit as spec,
// ignoring version words (those are chosen separately) and grade (the
// caller compares it).
//
// Shops disagree on whether a name prints the model number, so it can only
// be compared when both sides have one; otherwise the descriptive words
// decide. With loose set, spec's words need only all appear in other — what
// a user gets by trimming words in the watch editor.
func SameSuit(spec, other Key, loose bool) bool {
	if spec.Model != "" && other.Model != "" {
		if spec.Model != other.Model {
			return false
		}
		switch {
		case len(spec.Tokens) == 0 || len(other.Tokens) == 0:
			return true
		case loose:
			return intersect(spec.Tokens, other.Tokens) == len(spec.Tokens)
		}
		return overlap(spec.Tokens, other.Tokens) >= overlapAccept && jaccard(spec.Tokens, other.Tokens) >= overlapJaccardAccept
	}
	if len(spec.Tokens) == 0 {
		return false
	}
	if loose {
		return intersect(spec.Tokens, other.Tokens) == len(spec.Tokens)
	}
	return jaccard(spec.Tokens, other.Tokens) >= watchJaccard
}
