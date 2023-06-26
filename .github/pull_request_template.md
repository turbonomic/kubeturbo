  > Detailed information on writing code review requests can be found in ["Content of a Code Review Request"](https://vmturbo.atlassian.net/wiki/spaces/Home/pages/1184890931/Content+of+a+Code+Review+Request)
> ***REMOVE THIS SECTION WHEN EDITING YOUR MERGE REQUEST***

# Intent

_(what you hope to accomplish with this change)_

# Background

_(any background information needed to understand the change)_

# Testing

_(any manual resting performed, along with log outputs and screenshots as appropriate)_

# Checklist

These are the items that must be done by the developer and by reviewers before the change is ready to merge. Please ~~strikeout~~ any items that are not applicable, but don't delete them

- [ ] Developer Checks
    - [ ] Full build with unit tests and fmt and vet checks
    - [ ] Unit tests added / updated
    - [ ] No unlicensed images, no third-party code (such as from StackOverflow)
    - [ ] Integration tests added / updated
    - [ ] Manual testing done (and described)
    - [ ] Product sweep run and passed
    - [ ] Developer wiki updated (and linked to this description)
- [ ] Reviewer Checks
    - [ ] Merge request description clear and understandable
    - [ ] Developer checklist items complete
    - [ ] Functional code review (how is the code written)
    - [ ] Architectural review (does the code try to do the right thing, in the right way)
    - [ ] Defensive coding (incoming data checked / sanitized, exceptions logged, clear error messages)
    - [ ] No unlicensed images, no third-party code (such as from StackOverflow)
    - [ ] Security review checklist complete.

# Audience

_(@ mention any `review/...` groups or people that should be aware of this merge request)_
