package ttml

import "encoding/xml"

// Agent declarations and alternation follow the fixed AMLL TTML parser and its
// toAmllLyrics converter. Resolve them after parsing, while lines still retain
// source order; Finish later performs the separate stable playback-time sort.
func (d *documentParser) readAgentType(attrs []xml.Attr) {
	id, _ := attribute(attrs, xmlNS, "id")
	if id == "" {
		id, _ = timingAttribute(attrs, "id")
	}
	if id == "" {
		return
	}
	typeName, _ := attribute(attrs, ttmNS, "type")
	if typeName == "" {
		typeName, _ = timingAttribute(attrs, "type")
	}
	if d.agentTypes == nil {
		d.agentTypes = make(map[string]string)
	}
	// Repeated declarations replace earlier ones, matching the reference map.
	d.agentTypes[id] = typeName
}

func (d *documentParser) applyAgentDuets() error {
	var lastPersonID string
	hasLastPerson := false
	lastPersonDuet := false
	mainDuet := false
	for index := range d.lines {
		if index&255 == 0 {
			if err := d.p.Check(); err != nil {
				return err
			}
		}
		line := &d.lines[index]
		if line.Background {
			line.Duet = mainDuet
			continue
		}
		agentID := line.Agent
		if agentID == "" {
			agentID = "v1"
		}
		mainDuet = false
		if d.agentTypes[agentID] != "group" {
			switch {
			case !hasLastPerson:
				mainDuet = d.agentTypes[agentID] == "other"
			case lastPersonID == agentID:
				mainDuet = lastPersonDuet
			default:
				mainDuet = !lastPersonDuet
			}
			lastPersonID, lastPersonDuet, hasLastPerson = agentID, mainDuet, true
		}
		line.Duet = mainDuet
	}
	return d.p.Check()
}
