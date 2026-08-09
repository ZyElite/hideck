package imscore

import (
	"time"

	"github.com/emiago/sipgo/sip"
)

func (transaction *serverSIPTransaction) runFinalLifecycle(code int) {
	if !transaction.invite {
		transaction.waitAndFinish(transaction.nonInviteRetention())
		return
	}
	if code < 300 {
		transaction.waitAndFinish(transaction.timers.l)
		return
	}
	transaction.runRejectedInvite()
}

func (transaction *serverSIPTransaction) nonInviteRetention() time.Duration {
	if transaction.reliable {
		return 0
	}
	return transaction.timers.j
}

func (transaction *serverSIPTransaction) waitAndFinish(duration time.Duration) {
	if duration <= 0 {
		transaction.finish(false)
		return
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		transaction.finish(false)
	case <-transaction.done:
	}
}

func (transaction *serverSIPTransaction) runRejectedInvite() {
	h := time.NewTimer(transaction.timers.h)
	defer h.Stop()
	var retransmit *time.Timer
	interval := transaction.timers.t1
	if !transaction.reliable {
		retransmit = time.NewTimer(interval)
		defer retransmit.Stop()
	}
	for {
		select {
		case <-transaction.ack:
			transaction.waitAndFinish(transaction.inviteConfirmedRetention())
			return
		case <-timerChannel(retransmit):
			if err := transaction.replay(transaction.reply); err != nil {
				transaction.abort()
				return
			}
			interval = nextServerRetransmit(interval, transaction.timers.t2)
			retransmit.Reset(interval)
		case <-h.C:
			transaction.fail(sip.ErrTransactionTimeout, false)
			return
		case <-transaction.done:
			return
		}
	}
}

func (transaction *serverSIPTransaction) inviteConfirmedRetention() time.Duration {
	if transaction.reliable {
		return 0
	}
	return transaction.timers.i
}

func timerChannel(timer *time.Timer) <-chan time.Time {
	if timer == nil {
		return nil
	}
	return timer.C
}

func nextServerRetransmit(current, maximum time.Duration) time.Duration {
	next := current * 2
	if next > maximum {
		return maximum
	}
	return next
}
