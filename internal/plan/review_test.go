package plan_test

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
)

func TestFollowUpPrompt_Compose(t *testing.T) {
	t.Parallel()

	const paragraphFor = "Address every review comment in `%s` (attached to this run). " +
		"Each entry has the file, the side and line, the diff hunk it was left on, and the comment. " +
		"For each one, make the change, or say in your final message why you did not. " +
		"Leave code the comments do not concern alone, and commit your work to this branch when you are done."
	const attachment = "/runs/7.review-comments.json"
	paragraph := fmt.Sprintf(paragraphFor, attachment)
	const asking = "\n\n## Asking the owner\n\n" +
		"If you need a decision only the owner can make, write one question to /runs/7.question and end your turn. " +
		"The owner's answer resumes this session. Do not ask what you can decide yourself.\n\n" +
		"If code from a blocking ticket is missing from your branch, start the question with " +
		plan.MissingDependencyMarker + " and name what is missing. Do not implement the blocker's work yourself."
	fresh := plan.ComposeFollowUp

	tests := []struct {
		name string
		in   plan.FollowUpPrompt
		want string
	}{
		{name: "note only", in: plan.FollowUpPrompt{Message: "tidy up"}, want: fresh("tidy up")},
		{
			name: "comments only",
			in:   plan.FollowUpPrompt{Comments: 2, Attachment: attachment},
			want: fresh("Changes requested on this branch.\n\n" + paragraph),
		},
		{
			name: "note and comments",
			in:   plan.FollowUpPrompt{Message: " ship it ", Comments: 1, Attachment: attachment},
			want: fresh("Changes requested on this branch.\n\nship it\n\n" + paragraph),
		},
		{
			name: "resumed after a question",
			in:   plan.FollowUpPrompt{Message: "use sqlite", Resume: option.Some(plan.OutcomeAsked)},
			want: "The owner's answer to your question:\n\nuse sqlite",
		},
		{
			name: "resumed after an interrupt",
			in:   plan.FollowUpPrompt{Message: "stop", Resume: option.Some(plan.OutcomeContinued)},
			want: "The owner interrupted you with a message. Your last tool call may have been cut off, " +
				"so check its effect before going on:\n\nstop",
		},
		{
			name: "resumed with comments",
			in:   plan.FollowUpPrompt{Comments: 1, Attachment: attachment, Resume: option.Some(plan.OutcomeFailed)},
			want: "The owner's follow-up instruction:\n\nChanges requested on this branch.\n\n" + paragraph,
		},
		{
			name: "with AskPath",
			in:   plan.FollowUpPrompt{Message: "tidy up", AskPath: "/runs/7.question"},
			want: fresh("tidy up") + asking,
		},
		{
			name: "run id placeholder in the attachment path",
			in: plan.FollowUpPrompt{
				Comments: 1, Attachment: "/runs/" + plan.RunIDPlaceholder + "." + plan.ReviewAttachmentName,
			},
			want: fresh("Changes requested on this branch.\n\n" +
				"Address every review comment in `/runs/<run id>.review-comments.json` (attached to this run). " +
				"Each entry has the file, the side and line, the diff hunk it was left on, and the comment. " +
				"For each one, make the change, or say in your final message why you did not. " +
				"Leave code the comments do not concern alone, and commit your work to this branch when you are done."),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.want, tt.in.Compose()); diff != "" {
				t.Errorf("Compose() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
