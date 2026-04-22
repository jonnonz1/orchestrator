package task

import "testing"

func TestStore_PutGet(t *testing.T) {
	s := NewStore()

	task := &Task{ID: "abc", Prompt: "test prompt", Status: StatusPending}
	s.Put(task)

	got, err := s.Get("abc")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Prompt != "test prompt" {
		t.Errorf("Prompt = %q, want %q", got.Prompt, "test prompt")
	}
}

func TestStore_GetNotFound(t *testing.T) {
	s := NewStore()

	_, err := s.Get("nonexistent")
	if err == nil {
		t.Error("expected error for missing task")
	}
}

func TestStore_List(t *testing.T) {
	s := NewStore()

	s.Put(&Task{ID: "a", Status: StatusPending})
	s.Put(&Task{ID: "b", Status: StatusRunning})
	s.Put(&Task{ID: "c", Status: StatusCompleted})

	list := s.List()
	if len(list) != 3 {
		t.Errorf("List() len = %d, want 3", len(list))
	}
}

func TestStore_Overwrite(t *testing.T) {
	s := NewStore()

	s.Put(&Task{ID: "a", Status: StatusPending})
	s.Put(&Task{ID: "a", Status: StatusCompleted})

	got, _ := s.Get("a")
	if got.Status != StatusCompleted {
		t.Errorf("Status = %q, want completed", got.Status)
	}

	list := s.List()
	if len(list) != 1 {
		t.Errorf("List() len = %d, want 1", len(list))
	}
}

func TestTask_Defaults(t *testing.T) {
	task := &Task{}
	task.Defaults()

	if task.RamMB != 2048 {
		t.Errorf("RamMB = %d, want 2048", task.RamMB)
	}
	if task.VCPUs != 2 {
		t.Errorf("VCPUs = %d, want 2", task.VCPUs)
	}
	if task.OutputDir != "/root/output" {
		t.Errorf("OutputDir = %q, want /root/output", task.OutputDir)
	}
	if task.Timeout != 600 {
		t.Errorf("Timeout = %d, want 600", task.Timeout)
	}
}
