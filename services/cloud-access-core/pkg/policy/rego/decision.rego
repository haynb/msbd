package cloudaccess

default decision = {"allow": false, "reason": "default_deny", "rule_name": ""}

decision = result {
    deny_result[result]
}

decision = result {
    not has_deny
    allow_result[result]
}

decision = result {
    not has_deny
    not has_allow
    admin_result[result]
}

has_deny {
    deny_result[_]
}

has_allow {
    allow_result[_]
}

deny_result[result] {
    rule := input.rules[_]
    lower(rule.effect) == "deny"
    action_matches(rule.actions, input.action)
    resource_matches(rule.resources, input.resource)
    conditions_match(rule.conditions, input)
    result := {"allow": false, "reason": rule.name, "rule_name": rule.name}
}

allow_result[result] {
    rule := input.rules[_]
    lower(rule.effect) == "allow"
    action_matches(rule.actions, input.action)
    resource_matches(rule.resources, input.resource)
    conditions_match(rule.conditions, input)
    result := {"allow": true, "reason": rule.name, "rule_name": rule.name}
}

admin_result[result] {
    some i
    lower(input.roles[i]) == "admin"
    result := {"allow": true, "reason": "role_admin", "rule_name": "role_admin"}
}

action_matches(actions, action) {
    count(actions) == 0
}

action_matches(actions, action) {
    some i
    lower(actions[i]) == lower(action)
}

action_matches(actions, action) {
    some i
    actions[i] == "*"
}

resource_matches(resources, resource) {
    count(resources) == 0
}

resource_matches(resources, resource) {
    some i
    res := lower(resources[i])
    res == lower(resource)
}

resource_matches(resources, resource) {
    some i
    res := resources[i]
    endswith(res, "*")
    prefix := trim_suffix(res, "*")
    startswith(lower(resource), lower(prefix))
}

conditions_match(conditions, ctx) {
    conditions == null
}

conditions_match(conditions, ctx) {
    is_object(conditions)
    not conditions.roles
}

conditions_match(conditions, ctx) {
    is_object(conditions)
    conditions.roles
    some r
    required := lower(conditions.roles[r])
    some ur
    user := lower(ctx.roles[ur])
    required == user
}
