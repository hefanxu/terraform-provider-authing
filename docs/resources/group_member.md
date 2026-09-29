---
page_title: "authing_group_member Resource - terraform-provider-authing"
subcategory: ""
description: |-
  Binds a user to an Authing group.
---

# authing_group_member (Resource)

Binds a user to an Authing group.

## Example Usage

```terraform
resource "authing_group" "team" {
  code = "team_alpha"
  name = "Team Alpha"
}

resource "authing_user" "member" {
  username = "member01"
  email    = "member01@example.com"
}

resource "authing_group_member" "membership" {
  group_code = authing_group.team.code
  user_id    = authing_user.member.id
}
```

## Schema

### Required

- `group_code` (String, Forces New) Group code identifier.
- `user_id` (String, Forces New) User ID to add into the group.

### Read-Only

- `id` (String) Combined identifier (`group_code:user_id`).
