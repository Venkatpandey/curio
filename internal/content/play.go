package content

import "fmt"

// Quiz answers come from the story's cited sources or its stored coordinates.
type Quiz struct {
	Format      string   `json:"format,omitempty"`
	Question    string   `json:"question"`
	Options     []string `json:"options"`
	Answer      int      `json:"answer"`
	Explanation string   `json:"explanation"`
}

func (i Item) Quiz() Quiz {
	if i.Round != nil {
		return *i.Round
	}
	switch i.ID {
	case "venus":
		return Quiz{"photo", "Race time: which finishes first on Venus?", []string{"One trip around the Sun", "One full rotation", "They finish together"}, 0, "The orbit wins: about 225 Earth days, versus 243 for one rotation. Sunrise to sunrise is a different clock: about 117 days."}
	case "octopus":
		return Quiz{"photo", "Eight arms. How many hearts?", []string{"One very busy heart", "Three hearts", "Eight, one per arm"}, 1, "Three! Two pump blood through the gills. The third sends oxygenated blood around the body."}
	case "neutron":
		return Quiz{"photo", "The Crab pulsar spins… how often?", []string{"Once a day", "About 30 times a second", "Once every 30 years"}, 1, "About 30 rotations every second. The photograph shows the surrounding nebula, not the neutron star's surface."}
	case "socotra":
		return Quiz{"photo", "Why call this a dragon’s blood tree?", []string{"Its leaves turn red at night", "Dragons pollinated it", "It produces red resin"}, 2, "The name comes from its red resin. The umbrella-shaped crown is real, too."}
	case "tristan":
		return Quiz{"photo", "The volcano forced everyone to leave. What happened next?", []string{"The community returned", "The island became a theme park", "The settlement stayed empty"}, 0, "Residents evacuated in 1961. An advance party and then the wider community returned in 1963."}
	case "bryce":
		return Quiz{"photo", "Who sculpted these rock towers?", []string{"Ancient stoneworkers", "Repeated freezing and thawing", "Lava bubbling upward"}, 1, "Water enters cracks, freezes and expands. Repeated freeze-thaw cycles help break the rock into these irregular pillars, called hoodoos."}
	}
	if i.ArticleTitle != "" {
		for n, place := range Places {
			if place.Title != i.ArticleTitle {
				continue
			}
			options := []string{place.Region, Places[(n+7)%len(Places)].Region, Places[(n+13)%len(Places)].Region}
			answer := n % 3
			options[0], options[answer] = options[answer], options[0]
			return Quiz{"photo", "Pick a pin for this mystery photo.", options, answer, fmt.Sprintf("Meet %s, in %s. Now take another look at the photo.", i.Title, place.Region)}
		}
	}
	if i.Latitude != nil {
		answer := 0
		if *i.Latitude < 0 {
			answer = 1
		}
		return Quiz{"photo", "Trust your geography gut: which side of the equator?", []string{"Northern hemisphere", "Southern hemisphere"}, answer, fmt.Sprintf("%s is at latitude %.3f°. Positive latitude is north of the equator; negative is south.", i.Title, *i.Latitude)}
	}
	return Quiz{}
}

func (q Quiz) Label() string {
	switch q.Format {
	case "true-false":
		return "True or false"
	case "comparison":
		return "Pick a side"
	default:
		return "Photo mystery"
	}
}
