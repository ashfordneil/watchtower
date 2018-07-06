package actions

import (
	"math/rand"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/v2tec/watchtower/container"
)

var (
	letters  = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
)

// Update looks at the running Docker containers to see if any of the images
// used to start those containers have been updated. If a change is detected in
// any of the images, the new image is started, and then the old container is
// stopped
func Update(client container.Client, filter container.Filter, cleanup bool, noRestart bool, startTimeout time.Duration, stopTimeout time.Duration) error {
	log.Debug("Checking containers for updated images")

	containers, err := client.ListContainers(filter)
	if err != nil {
		return err
	}

	for i, container := range containers {
		stale, err := client.IsContainerStale(container)
		if err != nil {
			log.Infof("Unable to update container %s, err='%s'. Proceeding to next.", containers[i].Name(), err)
			stale = false
		}
		containers[i].Stale = stale
	}

	containers, err = container.SortByDependencies(containers)
	if err != nil {
		return err
	}

    checkDependencies(containers)

    // Start new versions of the stale containers (in sorted order). To prevent naming conflicts
    // with existing containers, rename the running version of the container to a random string
    // beforehand. If any error occurs starting up the new container, remove the stale label so
    // that the old container isn't killed
    for _, container := range containers {
        if !container.Stale {
            continue
        }

        if noRestart && !container.IsWatchtower() {
            continue
        }

        oldName := container.Name()
        if err := client.RenameContainer(container, randName()); err != nil {
            log.Error(err)
            container.Stale = false
            continue
        }

        if err := client.StartContainer(container, startTimeout); err != nil {
            log.Error(err)
            container.Stale = false

            // if we can't start the new container, undo the rename of the old container
            if err := client.RenameContainer(container, oldName); err != nil {
                log.Error(err)
            }

            continue
        }
    }


	// Stop stale containers in reverse order, deleting images as we go. If starting up the new
    // version of the container failed, the Stale flag will be false by this stage so we won't be
    // deleting containers that haven't been replaced (unless noRestart is set).
	for i := len(containers) - 1; i >= 0; i-- {
		container := containers[i]

		if container.IsWatchtower() {
			continue
		}

        if !container.Stale {
            continue
        }

        if err := client.StopContainer(container, stopTimeout); err != nil {
            log.Error(err)
            continue
        }

        if cleanup {
            if err := client.RemoveImage(container); err != nil {
                log.Error(err)
            }
        }
	}

	return nil
}

func checkDependencies(containers []container.Container) {

	for i, parent := range containers {
		if parent.Stale {
			continue
		}

	LinkLoop:
		for _, linkName := range parent.Links() {
			for _, child := range containers {
				if child.Name() == linkName && child.Stale {
					containers[i].Stale = true
					break LinkLoop
				}
			}
		}
	}
}

// Generates a random, 32-character, Docker-compatible container name.
func randName() string {
	b := make([]rune, 32)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}

	return string(b)
}
