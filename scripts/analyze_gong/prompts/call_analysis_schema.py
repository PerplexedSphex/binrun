from enum import Enum
from pydantic import BaseModel
from typing import List

class ParticipantRole(str, Enum):
    EXECUTIVE_BUYER = "Executive Buyer"
    PROGRAM_CHAMPION = "Program Champion"
    TECHNICAL_SPECIALIST = "Technical Specialist"
    FACILITY_USER = "Facility User"
    SELLER = "Seller"

class ProgramType(str, Enum):
    CHEMICALS = "Chemicals"
    WASTE = "Waste"

class Participant(BaseModel):
    name: str
    title: str
    role: ParticipantRole
    key_quotes: List[str]

class Program(BaseModel):
    name: str
    type: ProgramType
    scope: str
    processes: List[str]
    pain_points: List[str]
    priorities: List[str]

class CallAnalysis(BaseModel):
    participants: List[Participant]
    programs: List[Program]
    vendors: List[str]