package templates

type PolicyDocument struct {
	Id        string      `json:"Id,omitempty"`
	Statement []Statement `json:"Statement,omitempty"`
	Version   string      `json:"Version,omitempty"`
}

type Statement struct {
	Action    interface{} `json:"Action,omitempty"`
	Effect    string      `json:"Effect,omitempty"`
	Principal interface{} `json:"Principal,omitempty"`
	Resource  interface{} `json:"Resource,omitempty"`
	Sid       string      `json:"Sid,omitempty"`
}
